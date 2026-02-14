# Changelog

All notable changes to this project will be documented in this file.


## [v1.23.1] - 2026-02-13

This release introduces a new OpenAPI toolset for automatic API integration, task management capabilities, and several improvements to message handling and testing infrastructure.

## What's New

- Adds Tasks toolset with support for priorities and dependencies
- Adds OpenAPI built-in toolset type that automatically converts OpenAPI specifications into usable tools
- Adds support for custom telemetry tags via `TELEMETRY_TAGS` environment variable

## Improvements

- Preserves line breaks and indentation in welcome messages for better formatting
- Updates documentation links to point to GitHub Pages instead of code repository

## Bug Fixes

- Fixes recursive enforcement of required properties in OpenAI tool schemas (resolves Chrome MCP compatibility with OpenAI 5.2)
- Returns error when no messages are available after conversion instead of sending invalid requests

## Technical Changes

- Replaces time.Sleep in tests with deterministic synchronization for faster, more reliable testing
- Refactors models store implementation
- Adds .idea/ directory to gitignore
- Removes fake models.dev and unused code

### Pull Requests

- [#1704](https://github.com/docker/cagent/pull/1704) - Tasks toolset
- [#1710](https://github.com/docker/cagent/pull/1710) - fix: recursively enforce required properties in OpenAI tool schemas
- [#1714](https://github.com/docker/cagent/pull/1714) - docs: update CHANGELOG.md for v1.23.0
- [#1718](https://github.com/docker/cagent/pull/1718) - preserve line breaks and indentation in welcome messages
- [#1719](https://github.com/docker/cagent/pull/1719) - Add openapi built-in toolset type
- [#1720](https://github.com/docker/cagent/pull/1720) - return error if no messages are available after conversion
- [#1721](https://github.com/docker/cagent/pull/1721) - Refactor models store
- [#1722](https://github.com/docker/cagent/pull/1722) - Replace time.Sleep in tests with deterministic synchronization
- [#1723](https://github.com/docker/cagent/pull/1723) - Allow passing in custom tags to telemetry
- [#1724](https://github.com/docker/cagent/pull/1724) - Speed up fallback tests
- [#1726](https://github.com/docker/cagent/pull/1726) - Update documentation links to GitHub Pages


## [v1.23.0] - 2026-02-12

This release improves TUI display accuracy, enhances API security defaults, and fixes several memory leaks and session handling issues.

## What's New

- Adds optional setup script support for evaluation sessions to prepare container environments before agent execution
- Adds user_prompt tools to the planner for interactive user questions

## Improvements

- Makes session compaction non-blocking with spinner feedback instead of blocking the TUI render thread
- Returns error responses for unknown tool calls instead of silently skipping them
- Strips null values from MCP tool call arguments to fix compatibility with models like GPT-5.2
- Improves error handling and logging in evaluation judge with better error propagation and structured logging

## Bug Fixes

- Fixes incorrect tool count display in TUI when running in --remote mode
- Fixes tick leak that caused ~10% CPU usage when assistant finished answering
- Fixes session store leak and removes redundant session store methods
- Fixes A2A agent card advertising unroutable wildcard address by using localhost
- Fixes potential goroutine leak in monitorStdin
- Fixes Agents.UnmarshalYAML to properly reject unknown fields in agent configurations
- Persists tool call error state in session messages so failed tool calls maintain error status when sessions are reloaded

## Technical Changes

- Removes CORS middleware from 'cagent api' command
- Changes default binding from 0.0.0.0 to 127.0.0.1:8080 for 'cagent api', 'cagent a2a' and 'cagent mcp' commands
- Uses different default ports for better security
- Lists valid versions in unsupported config version error messages
- Adds the summary message as a user message during session compaction
- Propagates cleanup errors from fakeCleanup and recordCleanup functions
- Logs errors on log file close instead of discarding them

### Pull Requests

- [#1648](https://github.com/docker/cagent/pull/1648) - fix: show correct tool count in TUI when running in --remote mode
- [#1657](https://github.com/docker/cagent/pull/1657) - Better default security for cagent api|mcp|a2a
- [#1663](https://github.com/docker/cagent/pull/1663) - docs: update CHANGELOG.md for v1.22.0
- [#1668](https://github.com/docker/cagent/pull/1668) - Session store cleanup
- [#1669](https://github.com/docker/cagent/pull/1669) - Fix tick leak
- [#1673](https://github.com/docker/cagent/pull/1673) - eval: add optional setup script support for eval sessions
- [#1684](https://github.com/docker/cagent/pull/1684) - Fix Agents.UnmarshalYAML to reject unknown fields
- [#1685](https://github.com/docker/cagent/pull/1685) - Fix A2A agent card advertising unroutable wildcard address
- [#1686](https://github.com/docker/cagent/pull/1686) - Close the session
- [#1687](https://github.com/docker/cagent/pull/1687) - Make /compact non-blocking with spinner feedback
- [#1688](https://github.com/docker/cagent/pull/1688) - Remove redundant stdin nil check in api command
- [#1689](https://github.com/docker/cagent/pull/1689) - Return error response for unknown tool calls instead of silently skipping
- [#1692](https://github.com/docker/cagent/pull/1692) - Add documentation gh-pages
- [#1693](https://github.com/docker/cagent/pull/1693) - Add the summary message as a user message
- [#1694](https://github.com/docker/cagent/pull/1694) - Add more documentation
- [#1696](https://github.com/docker/cagent/pull/1696) - Fix MCP tool calls with gpt 5.2
- [#1697](https://github.com/docker/cagent/pull/1697) - Bump Go to 1.26.0
- [#1699](https://github.com/docker/cagent/pull/1699) - Fix issues found by the review agent
- [#1700](https://github.com/docker/cagent/pull/1700) - List valid versions in unsupported config version error
- [#1703](https://github.com/docker/cagent/pull/1703) - Bump direct Go dependencies
- [#1705](https://github.com/docker/cagent/pull/1705) - Improve the Planner
- [#1706](https://github.com/docker/cagent/pull/1706) - Improve error handling and logging in evaluation judge
- [#1711](https://github.com/docker/cagent/pull/1711) - Persist tool call error state in session messages


## [v1.22.0] - 2026-02-09

This release enhances the chat experience with history search functionality and improves file attachment handling, along with multi-turn conversation support for command-line operations.

## What's New

- Adds Ctrl+R reverse history search to the chat editor for quickly finding previous conversations
- Adds support for multi-turn conversations in `cagent exec`, `cagent run`, and `cagent eval` commands
- Adds support for queueing multiple messages with `cagent run question1 question2 ...`

## Improvements

- Improves file attachment handling by inlining text-based files and fixing placeholder stripping
- Refactors scrollbar into a reusable scrollview component for more consistent scrolling behavior across the interface

## Bug Fixes

- Fixes pasted attachments functionality
- Fixes persistence of multi_content for user messages to ensure attachment data is properly saved
- Fixes session browser shortcuts (star, filter, copy-id) to use Ctrl modifier, preventing conflicts with search input
- Fixes title generation spinner that could spin forever
- Fixes scrollview height issues when used with dialogs
- Fixes double @@ symbols when using file picker for @ attachments

## Technical Changes

- Updates OpenAI schema format handling to improve compatibility

### Pull Requests

- [#1630](https://github.com/docker/cagent/pull/1630) - feat: add Ctrl+R reverse history search
- [#1640](https://github.com/docker/cagent/pull/1640) - better file attachments
- [#1645](https://github.com/docker/cagent/pull/1645) - Prevent title generation spinner to spin forever
- [#1649](https://github.com/docker/cagent/pull/1649) - docs: update CHANGELOG.md for v1.21.0
- [#1650](https://github.com/docker/cagent/pull/1650) - OpenAI doesn't like those format indications on the schema
- [#1652](https://github.com/docker/cagent/pull/1652) - Fix: persist multi_content for user messages
- [#1654](https://github.com/docker/cagent/pull/1654) - Refactor scrollbar into more reusable `scrollview` component
- [#1656](https://github.com/docker/cagent/pull/1656) - fix: use ctrl modifier for session browser shortcuts to avoid search conflict
- [#1659](https://github.com/docker/cagent/pull/1659) - Fix pasted attachments
- [#1661](https://github.com/docker/cagent/pull/1661) - deleting version 2 so i can use permissions
- [#1662](https://github.com/docker/cagent/pull/1662) - Multi turn (cagent exec|run|eval)


## [v1.21.0] - 2026-02-09

This release adds a new generalist coding agent, improves agent configuration handling, and includes several bug fixes and UI improvements.

## What's New
- Adds a generalist coding agent for enhanced coding assistance
- Adds OCI artifact wrapper for spec-compliant manifest with artifactType

## Improvements
- Supports recursive ~/.agents/skills directory structure
- Wraps todo descriptions at word boundaries in sidebar for better display
- Preserves 429 error details on OpenAI for better error handling

## Bug Fixes
- Fixes subagent delegation and validates model outputs when transfer_task is called
- Fixes YAML parsing issue with unquoted strings containing special characters like colons

## Technical Changes
- Freezes config version v4 and bumps to v5

### Pull Requests

- [#1419](https://github.com/docker/cagent/pull/1419) - Help fix #1419
- [#1625](https://github.com/docker/cagent/pull/1625) - Add a generalist coding agent
- [#1631](https://github.com/docker/cagent/pull/1631) - Support recursive ~/.agents/skills
- [#1632](https://github.com/docker/cagent/pull/1632) - Help fix #1419
- [#1633](https://github.com/docker/cagent/pull/1633) - Add OCI artifact wrapper for spec-compliant manifest with artifactType
- [#1634](https://github.com/docker/cagent/pull/1634) - docs: update CHANGELOG.md for v1.20.6
- [#1635](https://github.com/docker/cagent/pull/1635) - Freeze v4 and bump config version to v5
- [#1637](https://github.com/docker/cagent/pull/1637) - Fix subagent logic
- [#1641](https://github.com/docker/cagent/pull/1641) - unquoted strings are fine until they contain special characters like :
- [#1643](https://github.com/docker/cagent/pull/1643) - Wrap todo descriptions at word boundaries in sidebar
- [#1646](https://github.com/docker/cagent/pull/1646) - Bump Go dependencies
- [#1647](https://github.com/docker/cagent/pull/1647) - Preserve 429 error details on OpenAI


## [v1.20.6] - 2026-02-07

This release introduces branching sessions, model fallbacks, and automated code quality scanning, along with performance improvements and enhanced file handling capabilities.

## What's New

- Adds branching sessions feature that allows editing previous messages to create new session branches without losing original conversation history
- Adds automated nightly codebase scanner with multi-agent architecture for detecting code quality issues and creating GitHub issues
- Adds model fallback system that automatically retries with alternative models when inference providers fail
- Adds skill invocation via slash commands for enhanced workflow automation
- Adds `--prompt-file` CLI flag for including file contents as system context
- Adds debug title command for troubleshooting session title generation

## Improvements

- Improves @ attachment performance to prevent UI hanging in large or deeply nested directories
- Switches to Anthropic Files API for file uploads instead of embedding content directly, dramatically reducing token usage
- Enhances scanner resilience and adds persistent memory system for learning from previous runs

## Bug Fixes

- Fixes tool calls score rendering in evaluations
- Fixes title generation for OpenAI and Gemini models
- Fixes GitHub Actions directory creation issues

## Technical Changes

- Refactors to use cagent's built-in memory system and text format for sub-agent output
- Enables additional golangci-lint linters and fixes code quality issues
- Simplifies PR review workflow by adopting reusable workflow from cagent-action
- Updates Model Context Protocol SDK and other dependencies

### Pull Requests

- [#1573](https://github.com/docker/cagent/pull/1573) - Automated nightly codebase scanner
- [#1578](https://github.com/docker/cagent/pull/1578) - Branching sessions on message edit
- [#1589](https://github.com/docker/cagent/pull/1589) - Model fallbacks
- [#1595](https://github.com/docker/cagent/pull/1595) - Simplifies PR review workflow by adopting the new reusable workflow from cagent-action
- [#1610](https://github.com/docker/cagent/pull/1610) - docs: update CHANGELOG.md for v1.20.5
- [#1611](https://github.com/docker/cagent/pull/1611) - Improve @ attachments perf 
- [#1612](https://github.com/docker/cagent/pull/1612) - Only create a new modelstore if none is given
- [#1613](https://github.com/docker/cagent/pull/1613) - [evals] Fix tool calls score rendering
- [#1614](https://github.com/docker/cagent/pull/1614) - Added space between release links
- [#1617](https://github.com/docker/cagent/pull/1617) - Opus 4.6
- [#1618](https://github.com/docker/cagent/pull/1618) - feat: add --prompt-file CLI flag for including file contents as system context
- [#1619](https://github.com/docker/cagent/pull/1619) - Update Nightly Scan Workflow
- [#1620](https://github.com/docker/cagent/pull/1620) - /attach use file upload instead of embedding in the context
- [#1621](https://github.com/docker/cagent/pull/1621) - Update Go deps
- [#1622](https://github.com/docker/cagent/pull/1622) - Add debug title command for session title generation
- [#1623](https://github.com/docker/cagent/pull/1623) - Add skill invocation via slash commands 
- [#1624](https://github.com/docker/cagent/pull/1624) - Fix schema and add drift test
- [#1627](https://github.com/docker/cagent/pull/1627) - Enable more linters and fix existing issues


## [v1.20.5] - 2026-02-05

This release improves stability for non-interactive sessions, updates the default Anthropic model to Claude Sonnet 4.5, and adds support for private GitHub repositories and standard agent directories.

## What's New

- Adds support for using agent YAML files from private GitHub repositories
- Adds support for standard `.agents/skills` directory structure
- Adds deepwiki integration to the librarian
- Adds timestamp tracking to runtime events
- Allows users to define their own default model in global configuration

## Improvements

- Updates default Anthropic model to Claude Sonnet 4.5
- Adds reason explanations when relevance checks fail during evaluations
- Persists ACP sessions to default SQLite database unless specified with `--session-db` flag
- Makes aliased agent paths absolute for better path resolution
- Produces session database for evaluations to enable investigation of results

## Bug Fixes

- Prevents panic when elicitation is requested in non-interactive sessions
- Fixes title generation hanging with Gemini 3 models by properly disabling thinking
- Fixes current agent display in TUI interface
- Prevents TUI dimensions from going negative when sidebar is collapsed
- Fixes flaky test issues

## Technical Changes

- Simplifies ElicitationRequestEvent check to reduce code duplication
- Allows passing additional environment variables to Docker when running evaluations
- Passes LLM as judge on full transcript for better evaluation accuracy


## [v1.20.4] - 2026-02-03

This release improves session handling with relative references and tool permissions, along with better table rendering in the TUI.

## What's New
- Adds support for relative session references in --session flag (e.g., `-1` for last session, `-2` for second to last)
- Adds "always allow this tool" option to permanently approve specific tools or commands for the session
- Adds granular permission patterns for shell commands that auto-approve specific commands while requiring confirmation for others

## Improvements
- Updates shell command selection to work with the new tool permission system
- Wraps tables properly in the TUI's experimental renderer to fit terminal width with smart column sizing

## Bug Fixes
- Fixes reading of legacy sessions
- Fixes getting sub-session errors where session was not found

## Technical Changes
- Adds test databases for better testing coverage
- Automatically runs PR reviewer for Docker organization members
- Exposes new approve-tool confirmation type via HTTP and ConnectRPC APIs


## [v1.20.3] - 2026-02-02

This release migrates PR review workflows to packaged actions and includes visual improvements to the Nord theme.

## Improvements
- Migrates PR review to packaged cagent-action sub-actions, reducing workflow complexity
- Changes code fences to blue color in Nord theme for better visual consistency

## Technical Changes
- Adds task rebuild when themes change to ensure proper theme updates
- Removes local development configuration that was accidentally committed


## [v1.20.2] - 2026-02-02

This release improves the tools system architecture and enhances TUI scrolling performance.

## Improvements
- Improves render and mouse scroll performance in the TUI interface

## Technical Changes
- Adds StartableToolSet and As[T] generic helper to tools package
- Adds capability interfaces for optional toolset features
- Adds ConfigureHandlers convenience function for tools
- Migrates StartableToolSet to tools package and cleans up ToolSet interface
- Removes BaseToolSet and DescriptionToolSet wrapper
- Reorganizes tool-related code structure


## [v1.20.1] - 2026-02-02

This release includes UI improvements, better error handling, and internal code organization enhancements.

## Improvements

- Changes audio listening shortcut from ctrl-k to ctrl-l (ctrl-k is now reserved for line editing)
- Improves title editing by allowing double-click anywhere on the title instead of requiring precise icon clicks
- Keeps footer unchanged when using /session or /new commands unless something actually changes
- Shows better error messages when using "auto" model with no available providers or when dmr is not available

## Bug Fixes

- Fixes flaky test that was causing CI failures
- Fixes `cagent new` command functionality
- Fixes title edit hitbox issues when title wraps to multiple lines

## Technical Changes

- Organizes TUI messages by domain concern
- Introduces SessionStateReader interface for read-only access
- Introduces Subscription type for cleaner animation lifecycle management
- Improves tool registry API with declarative RegisterAll method
- Introduces HitTest for centralized mouse target detection in chat
- Makes sidebar View() function pure by moving SetWidth to SetSize
- Introduces cmdbatch package for fluent command batching
- Organizes chat runtime event handlers by category
- Introduces subscription package for external event sources
- Separates CollapsedViewModel from rendering in sidebar
- Improves provider handling and error messaging


## [v1.20.0] - 2026-01-30

This release introduces editable session titles, custom TUI themes, and improved evaluation capabilities, along with database improvements and bug fixes.

## What's New
- Adds editable session titles with `/title` command and TUI support for renaming sessions
- Adds custom TUI theme support with built-in themes and hot-reloading capabilities
- Adds permissions view dialog for better visibility into agent permissions
- Adds concurrent LLM-as-a-judge relevance checks for faster evaluations
- Adds image cache to cagent eval for improved performance

## Improvements
- Makes slash commands searchable in the command palette
- Improves command palette with scrolling, mouse support, and dynamic resizing
- Adds validation error display in elicitation dialogs when Enter is pressed
- Adds Ctrl+z support for suspending TUI application to background
- Adds `--exit-on-stdin-eof` flag for better integration control
- Adds `--keep-containers` flag to cagent eval for debugging

## Bug Fixes
- Fixes auto-heal corrupted OCI local store by forcing re-pull when corruption is detected
- Fixes input token counting with Gemini models
- Fixes space key not working in elicitation text input fields
- Fixes session compaction issues
- Fixes stdin EOF checking to prevent cagent api from terminating unexpectedly in containers

## Technical Changes
- Extracts messages from sessions table into normalized session_items table
- Adds database backup and recovery on migration failure
- Maintains backward/forward compatibility for session data
- Removes ESC key from main status bar (now shown in spinner)
- Removes progress bar from cagent eval logs
- Sends mouse events to dialogs only when open


## [v1.19.7] - 2026-01-26

This release improves the user experience with better error handling and enhanced output formatting.

## Improvements
- Improves error handling and user feedback throughout the application
- Enhances output formatting for better readability and user experience

## Technical Changes
- Updates internal dependencies and build configurations
- Refactors code structure for improved maintainability
- Updates development and testing infrastructure


## [v1.19.6] - 2026-01-26

This release improves the user experience with better error handling and enhanced output formatting.

## Improvements
- Improves error handling and user feedback throughout the application
- Enhances output formatting for better readability and user experience

## Technical Changes
- Updates internal dependencies and build configurations
- Refactors code structure for better maintainability
- Updates development and testing infrastructure


## [v1.19.5] - 2026-01-22

This release improves the terminal user interface with better error handling and visual feedback, along with concurrency fixes and enhanced Docker authentication options.

## What's New

- Adds external command support for providing Docker access tokens
- Adds MCP Toolkit example for better integration guidance
- Adds realistic benchmark for markdown rendering performance testing

## Improvements

- Improves edit_file tool error rendering with consistent styling and single-line display
- Improves PR reviewer agent with Go-specific patterns and feedback learning capabilities
- Enhances collapsed reasoning blocks with fade-out animation for completed tool calls
- Makes dialog value changes clearer by indicating space key usage
- Adds dedicated pending response spinner with improved rendering performance

## Bug Fixes

- Fixes edit_file tool to skip diff rendering when tool execution fails
- Fixes concurrent access issues in user configuration aliases map
- Fixes style restoration after inline code blocks in markdown text
- Fixes model defaults when using the "router" provider to prevent erroneous thinking mode
- Fixes paste events incorrectly going to editor when dialog is open
- Fixes cassette recording functionality

## Technical Changes

- Adds clarifying comments for configuration and data directory paths
- Hides tools configuration interface
- Protects aliases map with mutex for thread safety


[v1.19.5]: https://github.com/docker/cagent/releases/tag/v1.19.5

[v1.19.6]: https://github.com/docker/cagent/releases/tag/v1.19.6

[v1.19.7]: https://github.com/docker/cagent/releases/tag/v1.19.7

[v1.20.0]: https://github.com/docker/cagent/releases/tag/v1.20.0

[v1.20.1]: https://github.com/docker/cagent/releases/tag/v1.20.1

[v1.20.2]: https://github.com/docker/cagent/releases/tag/v1.20.2

[v1.20.3]: https://github.com/docker/cagent/releases/tag/v1.20.3

[v1.20.4]: https://github.com/docker/cagent/releases/tag/v1.20.4

[v1.20.5]: https://github.com/docker/cagent/releases/tag/v1.20.5

[v1.20.6]: https://github.com/docker/cagent/releases/tag/v1.20.6

[v1.21.0]: https://github.com/docker/cagent/releases/tag/v1.21.0

[v1.22.0]: https://github.com/docker/cagent/releases/tag/v1.22.0

[v1.23.0]: https://github.com/docker/cagent/releases/tag/v1.23.0

[v1.23.1]: https://github.com/docker/cagent/releases/tag/v1.23.1
