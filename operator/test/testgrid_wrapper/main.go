package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/GoogleCloudPlatform/testgrid/metadata"
)

func main() {
	// Get git commit from environment variable
	commit := os.Getenv("GIT_COMMIT")
	if commit == "" {
		log.Printf("GIT_COMMIT environment variable is required")
		os.Exit(1)
	}

	pr := os.Getenv("GIT_PR")
	if pr == "" {
		log.Printf("GIT_PR environment variable is required")
		os.Exit(1)
	}

	// create the output by making a temporary directory
	tempDir, err := os.MkdirTemp("", "testgrid_wrapper")
	if err != nil {
		log.Printf("Failed to create temporary directory: %v", err)
		os.Exit(1)
	}

	// create a commit-specific directory under the temp directory
	cd := filepath.Join(tempDir, commit)
	err = os.Mkdir(cd, 0755)
	if err != nil {
		log.Printf("Failed to create commit directory: %v", err)
		os.Exit(1)
	}

	// create started.json
	startedJson := metadata.Started{
		Timestamp:  time.Now().Unix(),
		RepoCommit: commit,
		Pull:       pr,
	}
	startedJsonBytes, err := json.Marshal(startedJson)
	if err != nil {
		log.Printf("Failed to marshal started.json: %v", err)
		os.Exit(1)
	}
	err = os.WriteFile(filepath.Join(cd, "started.json"), startedJsonBytes, 0644)
	if err != nil {
		log.Printf("Failed to write started.json: %v", err)
		os.Exit(1)
	}

	// make an artifacts directory in the commit directory for junit.xml
	err = os.Mkdir(filepath.Join(cd, "artifacts"), 0755)
	if err != nil {
		log.Printf("Failed to create artifacts directory: %v", err)
		os.Exit(1)
	}

	// create the report.xml file in the temporary directory
	// Run the command: go test -v 2>&1 ./... | go-junit-report -set-exit-code
	cmd := exec.Command("bash", "-c", "go test -v 2>&1 ./... | go-junit-report -set-exit-code")

	// Run the command and capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Command failed with error: %v", err)
		log.Printf("Output: %s", string(output))
		os.Exit(1)
	}

	// write the junit.xml file to the artifacts directory
	err = os.WriteFile(filepath.Join(cd, "artifacts", "junit.xml"), output, 0644)
	if err != nil {
		log.Printf("Failed to write junit.xml: %v", err)
		os.Exit(1)
	}

	// create finished.json
	passed := true
	ts := time.Now().Unix()
	finishedJson := metadata.Finished{
		Timestamp: &ts,
		Passed:    &passed,
	}
	finishedJsonBytes, err := json.Marshal(finishedJson)
	if err != nil {
		log.Printf("Failed to marshal finished.json: %v", err)
		os.Exit(1)
	}
	err = os.WriteFile(filepath.Join(cd, "finished.json"), finishedJsonBytes, 0644)
	if err != nil {
		log.Printf("Failed to write finished.json: %v", err)
		os.Exit(1)
	}

	// print the commit directory
	fmt.Printf("Commit directory: %s\n", cd)
}
