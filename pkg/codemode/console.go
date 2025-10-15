package codemode

import (
	"fmt"
	"io"
)

func console(stdOut, stdErr io.Writer) map[string]func(args ...any) {
	return map[string]func(args ...any){
		"debug": func(args ...any) {
			fmt.Fprintln(stdOut, args...)
		},
		"info": func(args ...any) {
			fmt.Fprintln(stdOut, args...)
		},
		"log": func(args ...any) {
			fmt.Fprintln(stdOut, args...)
		},
		"trace": func(args ...any) {
			fmt.Fprintln(stdOut, args...)
		},
		"warn": func(args ...any) {
			fmt.Fprintln(stdOut, args...)
		},
		"error": func(args ...any) {
			fmt.Fprintln(stdErr, args...)
		},
	}
}
