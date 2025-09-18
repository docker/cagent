package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dario.cat/mergo"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"gopkg.in/yaml.v3"

	"github.com/docker/cagent/pkg/api"
	"github.com/docker/cagent/pkg/config"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/creator"
	"github.com/docker/cagent/pkg/desktop"
	"github.com/docker/cagent/pkg/oauth"
	"github.com/docker/cagent/pkg/oci"
	"github.com/docker/cagent/pkg/remote"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/teamloader"
)

type Server struct {
	e            *echo.Echo
	runtimes     map[string]runtime.Runtime
	sessionStore session.Store
	agentsDir    string
	runConfig    config.RuntimeConfig
	teams        map[string]*team.Team
}

type Opt func(*Server)

func WithFrontend(fsys fs.FS) Opt {
	return func(s *Server) {
		assetHandler := http.FileServer(http.FS(fsys))
		s.e.GET("/*", echo.WrapHandler(assetHandler))
	}
}

func WithAgentsDir(dir string) Opt {
	return func(s *Server) {
		s.agentsDir = dir
	}
}

func New(sessionStore session.Store, runConfig config.RuntimeConfig, teams map[string]*team.Team, opts ...Opt) *Server {
	e := echo.New()
	e.Use(middleware.CORS())
	e.Use(middleware.Logger())
	s := &Server{
		e:            e,
		runtimes:     make(map[string]runtime.Runtime),
		sessionStore: sessionStore,
		runConfig:    runConfig,
		teams:        teams,
	}

	for _, opt := range opts {
		opt(s)
	}

	group := e.Group("/api")

	// List all available agents
	group.GET("/agents", s.getAgents)
	// Get an agent by id
	group.GET("/agents/:id", s.getAgentConfig)
	// Edit an agent configuration by id
	group.PUT("/agents/config", s.editAgentConfig)
	// Create a new agent
	group.POST("/agents", s.createAgent)
	// Create a new agent manually with YAML configuration
	group.POST("/agents/config", s.createAgentConfig)
	// Import an agent from a file path
	group.POST("/agents/import", s.importAgent)
	// Export multiple agents as a zip file
	group.POST("/agents/export", s.exportAgents)
	// Pull an agent from a remote registry
	group.POST("/agents/pull", s.pullAgent)
	// Push an agent to a remote registry
	group.POST("/agents/push", s.pushAgent)
	// Delete an agent by file path
	group.DELETE("/agents", s.deleteAgent)
	// List all sessions
	group.GET("/sessions", s.getSessions)
	// Get a session by id
	group.GET("/sessions/:id", s.getSession)
	// Resume a session by id
	group.POST("/sessions/:id/resume", s.resumeSession)
	// Create a new session and run an agent loop
	group.POST("/sessions", s.createSession)
	// Delete a session
	group.DELETE("/sessions/:id", s.deleteSession)

	// Run an agent loop
	group.POST("/sessions/:id/agent/:agent", s.runAgent)
	group.POST("/sessions/:id/agent/:agent/:agent_name", s.runAgent)

	group.GET("/desktop/token", s.getDesktopToken)

	// Resume to start an OAuth flow
	group.POST("/:id/resumeStartOauth", s.resumeStartOauth)
	group.POST("/resumeCodeReceivedOauth", s.resumeCodeReceivedOauth)

	return s
}

func (s *Server) Serve(_ context.Context, ln net.Listener) error {
	srv := http.Server{
		Handler: s.e,
	}

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("Failed to start server", "error", err)
		return err
	}

	return nil
}

func (s *Server) getDesktopToken(c echo.Context) error {
	authToken := desktop.GetToken(c.Request().Context())
	return c.JSON(http.StatusOK, map[string]string{"token": authToken})
}

// API handlers

func (s *Server) getAgentConfig(c echo.Context) error {
	agentID := c.Param("id")

	path, err := s.secureAgentPath(agentID)
	if err != nil {
		slog.Error("Invalid agent ID", "agentID", agentID, "error", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid agent ID"})
	}

	cfg, err := config.LoadConfigSecure(path, s.agentsDir)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "agent not found"})
	}

	return c.JSON(http.StatusOK, cfg)
}

func (s *Server) editAgentConfig(c echo.Context) error {
	var req api.EditAgentConfigRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.Filename == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "filename is required"})
	}

	path, err := s.secureAgentPath(req.Filename)
	if err != nil {
		slog.Error("Invalid filename", "filename", req.Filename, "error", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid filename"})
	}

	// Check if the file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "agent not found"})
	}

	// Load the target file content
	currentConfig, err := config.LoadConfigSecure(path, s.agentsDir)
	if err != nil {
		slog.Error("Failed to load current config", "path", path, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load current configuration"})
	}

	// Merge the new content with the current one
	if err := mergo.Merge(currentConfig, req.AgentConfig, mergo.WithOverride); err != nil {
		slog.Error("Failed to apply new agent configuration", "path", path, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to apply new agent configuration"})
	}
	mergedConfig := *currentConfig

	// Read current file to preserve shebang and metadata structure
	currentContent, err := os.ReadFile(path)
	if err != nil {
		slog.Error("Failed to read agent file", "path", path, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read agent file"})
	}

	// Extract shebang and version lines if they exist
	shebang := ""
	versionLine := ""
	currentLines := strings.Split(string(currentContent), "\n")
	for i, line := range currentLines {
		if i == 0 && strings.HasPrefix(line, "#!/") {
			shebang = line + "\n"
		} else if strings.HasPrefix(line, "version:") {
			versionLine = line + "\n"
			break
		}
	}

	// Marshal the merged configuration to YAML
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	err = encoder.Encode(mergedConfig)
	if err != nil {
		slog.Error("Failed to marshal merged config to YAML", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to generate merged YAML configuration"})
	}
	encoder.Close()
	yamlData := buf.Bytes()

	// Combine shebang, version, and merged YAML content
	finalContent := shebang + versionLine
	if shebang != "" || versionLine != "" {
		finalContent += "\n"
	}
	finalContent += string(yamlData)

	// Write the updated configuration back to the file
	if err := os.WriteFile(path, []byte(finalContent), 0o644); err != nil {
		slog.Error("Failed to write agent file", "path", path, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to write agent file"})
	}

	// Reload the agent to update the in-memory configuration
	t, err := teamloader.Load(c.Request().Context(), path, s.runConfig)
	if err != nil {
		slog.Error("Failed to reload agent after edit", "path", path, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to reload agent configuration"})
	}

	// Update the teams map with the reloaded agent
	agentKey := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if oldTeam, exists := s.teams[agentKey]; exists {
		// Stop old team's toolsets before replacing
		if err := oldTeam.StopToolSets(); err != nil {
			slog.Error("Failed to stop old team toolsets", "agentKey", agentKey, "error", err)
		}
	}
	s.teams[agentKey] = t

	slog.Info("Agent configuration updated successfully", "path", path)
	return c.JSON(http.StatusOK, map[string]any{"message": "agent configuration updated successfully", "path": path, "config": mergedConfig})
}

func (s *Server) createAgent(c echo.Context) error {
	var req api.CreateAgentRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	prompt := req.Prompt

	out, path, err := creator.CreateAgent(c.Request().Context(), s.agentsDir, prompt, s.runConfig)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create agent"})
	}

	slog.Info("Agent created", "path", path, "out", out)
	if path == "" {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": out})
	}

	t, err := teamloader.Load(c.Request().Context(), path, s.runConfig)
	if err != nil {
		_ = os.Remove(path)
		slog.Error("Failed to load agent", "path", path, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load agent"})
	}

	s.teams[filepath.Base(path)] = t

	slog.Info("Agent loaded", "path", path, "out", out)
	return c.JSON(http.StatusOK, map[string]string{"path": path, "out": out})
}

func (s *Server) createAgentConfig(c echo.Context) error {
	var req api.CreateAgentConfigRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	// Validate required fields
	if req.Filename == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "filename is required"})
	}
	if req.Model == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "model is required"})
	}
	if req.Description == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "description is required"})
	}
	if req.Instruction == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "instruction is required"})
	}

	filename := req.Filename
	model := req.Model
	description := req.Description
	instruction := req.Instruction

	// Check if file already exists and generate alternative name if needed
	originalFilename := filename
	counter := 1
	for {
		path := filepath.Join(s.agentsDir, filename+".yaml")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		filename = fmt.Sprintf("%s_%d", originalFilename, counter)
		counter++
	}

	// Create the YAML configuration
	agentConfig := map[string]any{
		"agents": map[string]latest.AgentConfig{
			"root": {
				Model:       model,
				Description: description,
				Instruction: instruction,
				Toolsets: []latest.Toolset{
					{Type: "filesystem"},
					{Type: "shell"},
				},
			},
		},
	}

	// Marshal to YAML with custom indentation (2 spaces)
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(1)
	err := encoder.Encode(agentConfig)
	if err != nil {
		slog.Error("Failed to marshal agent config to YAML", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to generate YAML configuration"})
	}
	encoder.Close()
	yamlData := buf.Bytes()

	// Prepend shebang line to the YAML content
	shebang := "#!/usr/bin/env cagent run\nversion: \"1\"\n\n"
	finalContent := shebang + string(yamlData)

	// Write the file to the agents directory
	targetPath := filepath.Join(s.agentsDir, filename)
	if !strings.HasSuffix(targetPath, ".yaml") && !strings.HasSuffix(targetPath, ".yml") {
		targetPath += ".yaml"
	}

	if err := os.WriteFile(targetPath, []byte(finalContent), 0o644); err != nil {
		slog.Error("Failed to write agent file", "path", targetPath, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to write agent file"})
	}

	// Load the agent from the new location
	t, err := teamloader.Load(c.Request().Context(), targetPath, s.runConfig)
	if err != nil {
		// Clean up the file we just created if loading fails
		_ = os.Remove(targetPath)
		slog.Error("Failed to load agent from target path", "path", targetPath, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load agent from target path: " + err.Error()})
	}

	agentKey := strings.TrimSuffix(filepath.Base(targetPath), filepath.Ext(targetPath))
	s.teams[agentKey] = t

	slog.Info("Manual agent created successfully", "filepath", targetPath, "filename", filename)
	return c.JSON(http.StatusOK, map[string]string{
		"filepath": targetPath,
	})
}

func (s *Server) importAgent(c echo.Context) error {
	var req api.ImportAgentRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.FilePath == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file_path is required"})
	}

	validatedSourcePath, err := config.ValidatePathInDirectory(req.FilePath, "")
	if err != nil {
		slog.Error("Invalid source file path", "path", req.FilePath, "error", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid source file path"})
	}

	// Check if the file exists
	if _, err := os.Stat(validatedSourcePath); os.IsNotExist(err) {
		slog.Error("Agent file does not exist", "path", validatedSourcePath)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "agent file not found"})
	}

	// Validate it's a YAML file
	if !strings.HasSuffix(strings.ToLower(validatedSourcePath), ".yaml") && !strings.HasSuffix(strings.ToLower(validatedSourcePath), ".yml") {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file must be a YAML file (.yaml or .yml)"})
	}

	// First validate the agent configuration by loading it
	_, err = teamloader.Load(c.Request().Context(), validatedSourcePath, s.runConfig)
	if err != nil {
		slog.Error("Failed to load agent from file", "path", validatedSourcePath, "error", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to load agent configuration: " + err.Error()})
	}

	// Read the original file content
	fileContent, err := os.ReadFile(validatedSourcePath)
	if err != nil {
		slog.Error("Failed to read agent file", "path", validatedSourcePath, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read agent file: " + err.Error()})
	}

	// Create target file path in agents directory
	agentKey := strings.TrimSuffix(filepath.Base(validatedSourcePath), filepath.Ext(validatedSourcePath))
	targetPath := filepath.Join(s.agentsDir, agentKey+".yaml")

	// If target file already exists, generate an alternative name
	if _, err := os.Stat(targetPath); err == nil {
		agentKey += "_copy"
		targetPath = filepath.Join(s.agentsDir, agentKey+".yaml")

		// If the _copy version also exists, add numbers until we find an available name
		counter := 1
		for {
			if _, err := os.Stat(targetPath); os.IsNotExist(err) {
				break
			}
			agentKey = strings.TrimSuffix(filepath.Base(validatedSourcePath), filepath.Ext(validatedSourcePath)) + fmt.Sprintf("_copy_%d", counter)
			targetPath = filepath.Join(s.agentsDir, agentKey+".yaml")
			counter++
		}
		slog.Info("Target file exists, using alternative name", "original_key", strings.TrimSuffix(filepath.Base(validatedSourcePath), filepath.Ext(validatedSourcePath)), "new_key", agentKey)
	}

	// Write the file to the agents directory
	if err := os.WriteFile(targetPath, fileContent, 0o644); err != nil {
		slog.Error("Failed to write agent file to agents directory", "target", targetPath, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to write agent file to " + targetPath + ": " + err.Error()})
	}

	// Load the agent from the new location
	t, err := teamloader.Load(c.Request().Context(), targetPath, s.runConfig)
	if err != nil {
		// Clean up the file we just created if loading fails
		_ = os.Remove(targetPath)
		slog.Error("Failed to load agent from target path", "path", targetPath, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load agent from target path: " + err.Error()})
	}

	s.teams[agentKey] = t

	slog.Info("Agent imported successfully", "originalPath", validatedSourcePath, "targetPath", targetPath, "key", agentKey)
	return c.JSON(http.StatusOK, map[string]string{
		"originalPath": validatedSourcePath,
		"targetPath":   targetPath,
		"description":  t.Agent("root").Description(),
	})
}

func (s *Server) exportAgents(c echo.Context) error {
	// Create zip file in the agents directory
	zipFileName := fmt.Sprintf("agents_export_%d.zip", time.Now().Unix())
	zipPath := filepath.Join(s.agentsDir, zipFileName)

	// Create the zip file
	zipFile, err := os.Create(zipPath)
	if err != nil {
		slog.Error("Failed to create zip file", "path", zipPath, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create zip file"})
	}
	defer zipFile.Close()

	// Create a zip writer
	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// Walk through the agents directory and add files to zip
	err = filepath.Walk(s.agentsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Skip the zip file itself to avoid recursion
		if path == zipPath {
			return nil
		}

		// Only include YAML/YML files
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		// Get relative path for the zip entry
		relPath, err := filepath.Rel(s.agentsDir, path)
		if err != nil {
			return err
		}

		// Create zip entry
		zipEntry, err := zipWriter.Create(relPath)
		if err != nil {
			return err
		}

		// Read file content and write to zip
		fileContent, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		_, err = zipEntry.Write(fileContent)
		return err
	})
	if err != nil {
		_ = os.Remove(zipPath) // Clean up on error
		slog.Error("Failed to create agents export zip", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create agents export: " + err.Error()})
	}

	slog.Info("Agents exported successfully", "zipPath", zipPath, "agentsDir", s.agentsDir)
	return c.JSON(http.StatusOK, map[string]string{
		"zipPath":      zipPath,
		"zipFile":      zipFileName,
		"zipDirectory": filepath.Dir(zipPath),
		"agentsDir":    s.agentsDir,
		"createdAt":    time.Now().Format(time.RFC3339),
	})
}

func (s *Server) pullAgent(c echo.Context) error {
	var req api.PullAgentRequest
	if err := c.Bind(&req); err != nil {
		slog.Error("Failed to bind pull agent request", "error", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	slog.Info("Pulling agent", "name", req.Name)
	_, err := remote.Pull(req.Name)
	if err != nil {
		slog.Error("Failed to pull agent", "name", req.Name, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to pull agent"})
	}

	yamlFile, err := fromStore(req.Name)
	if err != nil {
		slog.Error("Failed to get agent yaml", "name", req.Name, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get agent yaml"})
	}

	agentName := strings.ReplaceAll(req.Name, "/", "_")
	fileName := filepath.Join(s.agentsDir, agentName+".yaml")

	if err := os.WriteFile(fileName, []byte(yamlFile), 0o644); err != nil {
		slog.Error("Failed to write agent yaml", "name", req.Name, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to write agent yaml to " + fileName + ": " + err.Error()})
	}

	t, err := teamloader.Load(c.Request().Context(), fileName, s.runConfig)
	if err != nil {
		slog.Error("Failed to load agent", "name", req.Name, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load agent"})
	}

	s.teams[agentName] = t

	return c.JSON(http.StatusOK, map[string]string{"name": agentName, "description": t.Agent("root").Description()})
}

func (s *Server) pushAgent(c echo.Context) error {
	var req api.PushAgentRequest
	if err := c.Bind(&req); err != nil {
		slog.Error("Failed to bind push agent request", "error", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.Filepath == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "filepath is required"})
	}

	if req.Tag == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "tag is required"})
	}

	// Validate the file path to prevent directory traversal attacks
	validatedFilepath, err := config.ValidatePathInDirectory(req.Filepath, "")
	if err != nil {
		slog.Error("Invalid file path", "path", req.Filepath, "error", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid file path"})
	}

	slog.Info("Building and pushing agent", "filepath", validatedFilepath, "tag", req.Tag)

	// Check if the file exists
	if _, err := os.Stat(validatedFilepath); os.IsNotExist(err) {
		slog.Error("Agent file does not exist", "path", validatedFilepath)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "agent file not found"})
	}

	// First, build the artifact
	store, err := content.NewStore()
	if err != nil {
		slog.Error("Failed to create content store", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create content store"})
	}

	digest, err := oci.PackageFileAsOCIToStore(validatedFilepath, req.Tag, store)
	if err != nil {
		slog.Error("Failed to build artifact", "filepath", validatedFilepath, "tag", req.Tag, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to build artifact"})
	}

	slog.Info("Artifact built successfully", "tag", req.Tag, "digest", digest)

	// Then, push the artifact
	if err := remote.Push(req.Tag); err != nil {
		slog.Error("Failed to push agent", "tag", req.Tag, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to push agent"})
	}

	slog.Info("Agent pushed successfully", "filepath", validatedFilepath, "tag", req.Tag, "digest", digest)
	return c.JSON(http.StatusOK, map[string]string{
		"filepath": validatedFilepath,
		"tag":      req.Tag,
		"digest":   digest,
	})
}

func (s *Server) deleteAgent(c echo.Context) error {
	var req api.DeleteAgentRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.FilePath == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file_path is required"})
	}

	err := s.validateAgentPath(req.FilePath)
	if err != nil {
		slog.Error("Invalid file path", "path", req.FilePath, "error", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid file path"})
	}

	// Check if the file exists
	if _, err := os.Stat(req.FilePath); os.IsNotExist(err) {
		slog.Error("Agent file does not exist", "path", req.FilePath)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "agent file not found"})
	}

	// Validate it's a YAML file
	if !strings.HasSuffix(strings.ToLower(req.FilePath), ".yaml") && !strings.HasSuffix(strings.ToLower(req.FilePath), ".yml") {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file must be a YAML file (.yaml or .yml)"})
	}

	// Determine the agent key from the file path
	agentKey := strings.TrimSuffix(filepath.Base(req.FilePath), filepath.Ext(req.FilePath))

	// Remove from teams map and stop toolsets if active
	if t, exists := s.teams[agentKey]; exists {
		slog.Info("Stopping toolsets for agent", "agentKey", agentKey)
		if err := t.StopToolSets(); err != nil {
			slog.Error("Failed to stop tool sets for agent", "agentKey", agentKey, "error", err)
			// Continue with deletion even if stopping toolsets fails
		}
		delete(s.teams, agentKey)
		slog.Info("Removed agent from teams", "agentKey", agentKey)
	}

	// Delete the file
	if err := os.Remove(req.FilePath); err != nil {
		slog.Error("Failed to delete agent file", "path", req.FilePath, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete agent file: " + err.Error()})
	}

	slog.Info("Agent deleted successfully", "filePath", req.FilePath, "agentKey", agentKey)
	return c.JSON(http.StatusOK, map[string]string{
		"filePath": req.FilePath,
	})
}

func (s *Server) getAgents(c echo.Context) error {
	// Refresh agents from disk to get the latest configurations
	if err := s.refreshAgentsFromDisk(c.Request().Context()); err != nil {
		slog.Error("Failed to refresh agents from disk", "error", err)
	}

	agentList := make([]map[string]any, 0)
	for id, t := range s.teams {
		a := t.Agent("root")
		if a == nil {
			slog.Error("Agent root not found", "team", id)
			continue
		}
		agentList = append(agentList, map[string]any{
			"name":        id,
			"description": a.Description(),
			"multi":       a.HasSubAgents(),
		})
	}
	return c.JSON(http.StatusOK, agentList)
}

func (s *Server) refreshAgentsFromDisk(ctx context.Context) error {
	if s.agentsDir == "" {
		return nil
	}

	newTeams, err := teamloader.LoadTeams(ctx, s.agentsDir, s.runConfig)
	if err != nil {
		return fmt.Errorf("failed to load teams: %w", err)
	}

	for id, oldTeam := range s.teams {
		if _, exists := newTeams[id]; !exists {
			// Team no longer exists on disk, stop its tool sets
			if err := oldTeam.StopToolSets(); err != nil {
				slog.Error("Failed to stop tool sets for removed team", "team", id, "error", err)
			}
		}
	}

	s.teams = newTeams
	return nil
}

func (s *Server) getSessions(c echo.Context) error {
	sessions, err := s.sessionStore.GetSessions(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get sessions"})
	}

	responses := make([]api.SessionsResponse, len(sessions))
	for i, sess := range sessions {
		responses[i] = api.SessionsResponse{
			ID:                         sess.ID,
			Title:                      sess.Title,
			CreatedAt:                  sess.CreatedAt.Format(time.RFC3339),
			NumMessages:                len(sess.GetAllMessages()),
			InputTokens:                sess.InputTokens,
			OutputTokens:               sess.OutputTokens,
			GetMostRecentAgentFilename: sess.GetMostRecentAgentFilename(),
		}
	}
	return c.JSON(http.StatusOK, responses)
}

func (s *Server) createSession(c echo.Context) error {
	var sessionTemplate session.Session
	if err := c.Bind(&sessionTemplate); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	sess := session.New()
	sess.ToolsApproved = sessionTemplate.ToolsApproved

	if err := s.sessionStore.AddSession(c.Request().Context(), sess); err != nil {
		slog.Error("Failed to persist session", "session_id", sess.ID, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
	}

	return c.JSON(http.StatusOK, sess)
}

func (s *Server) getSession(c echo.Context) error {
	sess, err := s.sessionStore.GetSession(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "session not found"})
	}

	sr := api.SessionResponse{
		ID:            sess.ID,
		Title:         sess.Title,
		CreatedAt:     sess.CreatedAt,
		Messages:      sess.GetAllMessages(),
		ToolsApproved: sess.ToolsApproved,
		InputTokens:   sess.InputTokens,
		OutputTokens:  sess.OutputTokens,
	}

	return c.JSON(http.StatusOK, sr)
}

func (s *Server) resumeSession(c echo.Context) error {
	sessionID := c.Param("id")
	var req api.ResumeSessionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	rt, exists := s.runtimes[sessionID]
	if !exists {
		return c.JSON(http.StatusNotFound, map[string]string{"error": fmt.Sprintf("runtime not found: %s", sessionID)})
	}

	rt.Resume(c.Request().Context(), req.Confirmation)

	return c.JSON(http.StatusOK, map[string]string{"message": "session resumed"})
}

func (s *Server) deleteSession(c echo.Context) error {
	if err := s.sessionStore.DeleteSession(c.Request().Context(), c.Param("id")); err != nil {
		slog.Error("Failed to delete session", "session_id", c.Param("id"), "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete session"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "session deleted"})
}

func (s *Server) runAgent(c echo.Context) error {
	sessionID := c.Param("id")
	agentFilename := c.Param("agent")
	currentAgent := c.Param("agent_name")
	if currentAgent == "" {
		currentAgent = "root"
	}

	slog.Debug("Running agent", "agent_filename", agentFilename, "session_id", sessionID, "current_agent", currentAgent)

	t, exists := s.teams[agentFilename]
	if !exists {
		return c.JSON(http.StatusNotFound, map[string]string{"error": fmt.Sprintf("runtime not found: %s", agentFilename)})
	}
	sess, err := s.sessionStore.GetSession(c.Request().Context(), sessionID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "session not found"})
	}

	rt, exists := s.runtimes[sess.ID]
	if !exists {
		var opts []runtime.Opt = []runtime.Opt{
			runtime.WithCurrentAgent(currentAgent),
		}
		rt, err = runtime.New(t, opts...)
		if err != nil {
			slog.Error("Failed to create runtime", "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create runtime"})
		}
		s.runtimes[sess.ID] = rt
	}

	var messages []api.Message
	if err := json.NewDecoder(c.Request().Body).Decode(&messages); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	// TODO(dga): for now, we only receive one message and it's always a user message.
	for _, msg := range messages {
		sess.AddMessage(session.UserMessage(agentFilename, msg.Content))
	}

	if err := s.sessionStore.UpdateSession(c.Request().Context(), sess); err != nil {
		slog.Error("Failed to update session in store", "session_id", sess.ID, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update session"})
	}

	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().WriteHeader(http.StatusOK)

	streamChan := rt.RunStream(c.Request().Context(), sess)
	for event := range streamChan {
		data, err := json.Marshal(event)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to marshal event"})
		}
		fmt.Fprintf(c.Response(), "data: %s\n\n", string(data))
		c.Response().Flush()
	}

	if err := s.sessionStore.UpdateSession(c.Request().Context(), sess); err != nil {
		slog.Error("Failed to final update session in store", "session_id", sess.ID, "error", err)
	}

	return nil
}

func fromStore(reference string) (string, error) {
	store, err := content.NewStore()
	if err != nil {
		return "", err
	}

	img, err := store.GetArtifactImage(reference)
	if err != nil {
		return "", err
	}

	layers, err := img.Layers()
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	layer := layers[0]
	b, err := layer.Uncompressed()
	if err != nil {
		return "", err
	}

	_, err = io.Copy(&buf, b)
	if err != nil {
		return "", err
	}
	b.Close()

	return buf.String(), nil
}

func (s *Server) validateAgentPath(path string) error {
	validatedPath, err := config.ValidatePathInDirectory(path, s.agentsDir)
	if err != nil {
		return fmt.Errorf("invalid agent file path: %w", err)
	}

	absOriginal, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve original path: %w", err)
	}

	if absOriginal != validatedPath {
		return fmt.Errorf("path validation mismatch: security check failed")
	}

	return nil
}

func (s *Server) secureAgentPath(filename string) (string, error) {
	if !strings.HasSuffix(filename, ".yaml") && !strings.HasSuffix(filename, ".yml") {
		filename += ".yaml"
	}

	return config.ValidatePathInDirectory(filename, s.agentsDir)
}

func (s *Server) resumeStartOauth(c echo.Context) error {
	sessionID := c.Param("id")
	var req api.ResumeStartOauthRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	rt, exists := s.runtimes[sessionID]
	if !exists {
		return c.JSON(http.StatusNotFound, map[string]string{"error": fmt.Sprintf("runtime not found: %s", sessionID)})
	}

	rt.ResumeStartAuthorizationFlow(c.Request().Context(), req.Confirmation)

	return c.JSON(http.StatusOK, map[string]string{"message": "Oauth started"})
}

func (s *Server) resumeCodeReceivedOauth(c echo.Context) error {
	var req api.ResumeCodeReceivedOauthRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	code := req.Code
	state := req.State

	// Extract session ID from the OAuth state parameter
	sessionID, err := oauth.DecodeSessionIDFromState(state)
	if err != nil {
		slog.Error("Failed to decode session ID from OAuth state", "error", err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid OAuth state parameter"})
	}

	rt, exists := s.runtimes[sessionID]
	if !exists {
		return c.JSON(http.StatusNotFound, map[string]string{"error": fmt.Sprintf("runtime not found: %s", sessionID)})
	}

	// Send the authorization code to the runtime's OAuth channel
	if err := rt.ResumeCodeReceived(c.Request().Context(), code); err != nil {
		slog.Error("Failed to send OAuth code to runtime", "session_id", sessionID, "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "OAuth flow not in progress"})
	}

	slog.Debug("OAuth authorization code sent to runtime", "session_id", sessionID)

	return c.JSON(http.StatusOK, map[string]string{"message": "OAuth code received"})
}
