# go-nsenter

[![GoDoc](https://godoc.org/github.com/Devatoria/go-nsenter?status.svg)](https://godoc.org/github.com/Devatoria/go-nsenter)

Golang library to execute program into process namespaces using `nsenter`.

## Example

```go
package main

import (
	"fmt"

	"github.com/Devatoria/go-nsenter"
)

func main() {
	config := &nsenter.Config{
		Mount:  true, // Execute into mount namespace
		Target: 1,    // Enter into PID 1 (init) namespace
	}

	stdout, stderr, err := config.Execute("ls", "-la")
	if err != nil {
		fmt.Println(stderr)
		panic(err)
	}

	fmt.Println(stdout)
}
```

## Features

- [X] Allow to specify a file to use to enter namespace
- [X] Return stdout/stderr of the executed command
