package main

import (
	"flag"
	"os"
)

var (
	endpointName = flag.String("endpoint", "", "endpoint name override")
	modelName    = flag.String("model", "", "model override")
	yolo         = flag.Bool("yolo", false, "disable all approval prompts")
	resume       = flag.Bool("resume", false, "resume a past session")
)

func main() {
	flag.Parse()
	os.Exit(0)
}
