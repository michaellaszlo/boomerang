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

To build a Boomerang website, navigate to its root directory and execute
`buildapp`.


## Small example

Write a top-level Boomerang template called `index.boo`:

    <?code
      package main

      func main() {
        x := 2
    ?>
      <?insert header.mer ?>

      <h1> Hello, world. </h1>

      <p> This is a minimal web app. </p>

      <p> <?code runtime.Printf("x = %d", x) ?> </p>

    <?insert footer.mer ?>
    <?code
      }
    ?>

The `.boo` suffix indicates that this Boomerang template contains
the entry point to a Go program. Our `index.boo` template imports two
lower-level templates named `header.mer` and `footer.mer`.

Write the following into `header.mer`:

    <!DOCTYPE html>
    <head>
      <title> Small example </title>
    </head>
    <body>

And the following into `footer.mer`:

    </body>
    </html>

Now you're ready to build the app. Execute `buildapp` (assuming you've
moved it into a directory named `~/bin`):

    ~/bin/buildapp

You should see these messages (with your own different directory name):

    recursive walk from /var/www/example
    created /var/www/example/index.go
    parsing /var/www/example/index.boo
    compiling /var/www/example/index.go

Now you have a binary file called `index.cgi`. You can run it to generate
a web page:

    ./index.cgi


## Elaborate example

Please see my
[dictionary-website-boomerang](https://github.com/michaellaszlo/dictionary-website-boomerang)
repository for an example of a non-trivial website written with Boomerang.

