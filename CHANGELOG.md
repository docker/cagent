# Changelog

All notable changes to this project will be documented in this file.


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
