package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
)

type PreProcess struct {
	Cmd struct {
		Name string   `json:"name"`
		Args []string `json:"args,omitempty"`
	} `json:"cmd"`
}

type preProcessError struct {
	Error  string `json:"error"`
	StdErr string `json:"stderr"`
}

func (proc *PreProcess) RunPreProcess(response Response) (Response, error) {

	var (
		stdout     bytes.Buffer
		stderr     bytes.Buffer
		bodyReader *bytes.Reader
		cmd        *exec.Cmd
	)

	if proc.Cmd.Name == "" {
		return response, fmt.Errorf("Invalid cmd for pre_process: must not be an empty string")
	}

	bodyReader = bytes.NewReader(response.body)

	cmd = exec.Command(proc.Cmd.Name)
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	cmd.Stdin = bodyReader
	cmd.Args = append(cmd.Args, proc.Cmd.Args...)

	err := cmd.Run()
	if err != nil {
		var err2 error
		response.body, err2 = json.MarshalIndent(preProcessError{
			Error:  err.Error(),
			StdErr: string(stderr.Bytes()),
		}, "", "  ")
		return response, err2
	}

	response.body = stdout.Bytes()
	return response, nil
}
