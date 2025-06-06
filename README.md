# Gest
One of the joys of working with JavaScript—especially with Jest—is the delightful, informative test output it provides. These clear and visually appealing results make debugging and development much more enjoyable.

However, when migrating from JavaScript to Go, the default go test output can feel underwhelming and hard to interpret. This project bridges that gap by taking the JSON output from `go test -json` and the coverage data from `-coverprofile`, then transforming them into a Jest-like experience—no need to modify your Go tests!

## Usage

`go test -json | gest`

or 

`go test -coverprofile=myfile.out | gtest -c=myfile.out`
