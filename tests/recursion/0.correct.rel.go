package main

import "fmt"

func main() {
  makeLastInsertionAbsolute := false
  fmt.Print(`<p> 0 </p>

  <p> 1 </p>

      <p> 3 </p>

        <p> 4 </p>
`)
  if makeLastInsertionAbsolute {
    fmt.Print(`
    <p> 2 </p>

`)
  } else {
    fmt.Print(`
    <p> 2 </p>
`)
  }

}
