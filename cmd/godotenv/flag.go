package main

import "strings"

type stringsFlag []string

func (f *stringsFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func (f stringsFlag) String() string {
	return strings.Join(f, ",")
}
