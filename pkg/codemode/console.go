package codemode

import (
	"fmt"
	"os"
)

func console() map[string]any {
	return map[string]any{
		"debug": func(args ...any) {
			fmt.Fprintln(os.Stdout, args...)
		},
		"error": func(args ...any) {
			fmt.Fprintln(os.Stdout, args...)
		},
		"info": func(args ...any) {
			fmt.Fprintln(os.Stdout, args...)
		},
		"log": func(args ...any) {
			fmt.Fprintln(os.Stdout, args...)
		},
		"trace": func(args ...any) {
			fmt.Fprintln(os.Stdout, args...)
		},
		"warn": func(args ...any) {
			fmt.Fprintln(os.Stdout, args...)
		},
	}
}
