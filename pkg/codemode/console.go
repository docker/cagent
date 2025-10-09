package codemode

import (
	"fmt"
	"os"
)

func console() map[string]any {
	return map[string]any{
		"debug": console_debug,
		"error": console_error,
		"info":  console_info,
		"log":   console_log,
		"trace": console_trace,
		"warn":  console_warn,
	}
}

func console_debug(args ...any) { //nolint:staticcheck // match JavaScript's console method names.
	fmt.Fprintln(os.Stdout, args...)
}

func console_error(args ...any) { //nolint:staticcheck // match JavaScript's console method names.
	fmt.Fprintln(os.Stdout, args...)
}

func console_info(args ...any) { //nolint:staticcheck // match JavaScript's console method names.
	fmt.Fprintln(os.Stdout, args...)
}

func console_log(args ...any) { //nolint:staticcheck // match JavaScript's console method names.
	fmt.Fprintln(os.Stdout, args...)
}

func console_trace(args ...any) { //nolint:staticcheck // match JavaScript's console method names.
	fmt.Fprintln(os.Stdout, args...)
}

func console_warn(args ...any) { //nolint:staticcheck // match JavaScript's console method names.
	fmt.Fprintln(os.Stdout, args...)
}
