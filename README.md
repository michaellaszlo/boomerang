# Boomerang

## A system for writing PHP-style web templates using Go

Boomerang is a web template system that uses Go. It aims to replicate
the basic functionality of PHP. To write a web page with Boomerang,
you interleave static HTML with sections of HTML-generating Go code. The
resulting file is a Boomerang template.

Because Go is a compiled language, the Boomerang template must be compiled
into an executable file that generates the web page on demand. The
`buildapp` program included in this repository crawls a web directory
recursively and compiles each Boomerang template into an `index.cgi`
binary.


## Deployment

Install [Go](https://golang.org/doc/install) and configure a [Go
workspace](https://golang.org/doc/code.html).

Get the [astutil
package](https://godoc.org/golang.org/x/tools/go/ast/astutil), which is
a Boomerang dependency:

    go get golang.org/x/tools/go/ast/astutil

Get the [Boomerang
files](https://godoc.org/github.com/michaellaszlo/boomerang):

    go get github.com/michaellaszlo/boomerang

Build the `buildapp` command (assuming that the Go workspace is in `go/`):

    go build go/src/github.com/michaellaszlo/boomerang/buildapp/buildapp.go

To build a Boomerang website, navigate to its root directory and run
`buildapp`.


