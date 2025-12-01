package fsx

import (
	"os"
	"path/filepath"
)

type TreeNode struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Children []*TreeNode `json:"children,omitempty"`
}

func DirectoryTree(path string, shouldIgnore func(string) bool, maxItems int) (*TreeNode, error) {
	itemCount := 0
	return directoryTree(path, shouldIgnore, maxItems, &itemCount)
}

func directoryTree(path string, shouldIgnore func(string) bool, maxItems int, itemCount *int) (*TreeNode, error) {
	if maxItems > 0 && *itemCount >= maxItems {
		return nil, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	node := &TreeNode{
		Name: filepath.Base(path),
		Type: "file",
	}

	*itemCount++

	if info.IsDir() {
		node.Type = "directory"
		node.Children = []*TreeNode{}

		entries, err := os.ReadDir(path)
		if err != nil {
			return node, nil // Return partial result on error
		}

		for _, entry := range entries {
			childPath := filepath.Join(path, entry.Name())
			// Skip if should be ignored (VCS, gitignore, etc.)
			if shouldIgnore != nil && shouldIgnore(childPath) {
				continue
			}

			childNode, err := directoryTree(childPath, shouldIgnore, maxItems, itemCount)
			if err != nil || childNode == nil {
				continue
			}
			node.Children = append(node.Children, childNode)
		}
	}

	return node, nil
}

func ListDirectory(path string, shouldIgnore func(string) bool) ([]string, error) {
	tree, err := DirectoryTree(path, shouldIgnore, 0)
	if err != nil {
		return nil, err
	}

	var files []string
	CollectFilesFromTree(tree, "", &files)
	return files, nil
}

// CollectFilesFromTree recursively collects file paths from a DirectoryTree.
// Pass basePath="" for relative paths, or a parent directory for absolute paths.
func CollectFilesFromTree(node *TreeNode, basePath string, files *[]string) {
	if node == nil {
		return
	}
	fullPath := filepath.Join(basePath, node.Name)
	switch node.Type {
	case "file":
		*files = append(*files, fullPath)
	case "directory":
		for _, child := range node.Children {
			CollectFilesFromTree(child, fullPath, files)
		}
	}
}
