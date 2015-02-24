package main

import "fmt"

func main() {
  isCorrect := true
  lastInsertIsAbsolute := true
  fmt.Print(`<p> 0 </p>

    <p> 1 </p>

`)
  if isCorrect {
    fmt.Print(`
?>
      <p> 3 </p>

`)
  } else {
    fmt.Print(`
      <p> 2 </p>
`)
  }

}
