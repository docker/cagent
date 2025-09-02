package tools

func DisplayName(tool string) string {
	switch tool {
	// file tools
	case "add_allowed_directory":
		return "Add Allowed Directory"
	case "create_directory":
		return "Create Directory"
	case "directory_tree":
		return "Directory Tree"
	case "edit_file":
		return "Edit File"
	case "get_file_info":
		return "Get File Info"
	case "list_allowed_directories":
		return "List Allowed Directories"
	case "list_directory_with_sizes":
		return "List Directory With Sizes"
	case "list_directory":
		return "List Directory"
	case "move_file":
		return "Move File"
	case "read_file":
		return "Read File"
	case "read_multiple_files":
		return "Read Multiple Files"
	case "search_files_content":
		return "Search Files Content"
	case "search_files":
		return "Search Files"
	case "write_file":
		return "Write File"

	// memory tools
	case "add_memory":
		return "Add Memory"
	case "get_memories":
		return "Get Memories"
	case "delete_memory":
		return "Delete Memory"

	// shell tools
	case "shell":
		return "Run Shell Command"
	case "get_command_output":
		return "Get Command Output"

	// think tools
	case "think":
		return "Think"

	// todo tools
	case "create_todo":
		return "Create TODO"
	case "create_todos":
		return "Create TODOs"
	case "update_todo":
		return "Update TODO"
	case "list_todos":
		return "List TODOs"

	// transfer_task tools
	case "transfer_task":
		return "Transfer Task"
	}

	return tool
}
