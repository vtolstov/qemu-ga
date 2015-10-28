package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os/exec"
	"strings"
	"syscall"
)

var cmdExec = &Command{
	Name:    "guest-exec",
	Func:    fnExec,
	Enabled: true,
	Returns: true,
}

func init() {
	commands = append(commands, cmdExec)
}

func fnExec(req *Request) *Response {
	res := &Response{Id: req.Id}

	reqData1 := struct {
		Command string `json:"command"`
	}{}
	reqData2 := struct {
		Path   string `json:"path"`
		Arg    string `json:"arg,omitempty"`
		Env    string `json:"env,omitempty"`
		Input  string `json:"input-data,omitempty"`
		Output bool   `json:"capture-output"`
	}{}

	var errStr []string

	if err := json.Unmarshal(req.RawArgs, &reqData2); err == nil {
		goto exec2
	} else {
		errStr = append(errStr, err.Error())
	}

	if err := json.Unmarshal(req.RawArgs, &reqData1); err == nil {
		goto exec1
	} else {
		errStr = append(errStr, err.Error())
	}

	res.Error = &Error{Code: -1, Desc: strings.Join(errStr, ";")}
	return res

exec1:
	return fnExec1(req)
exec2:
	return fnExec2(req)
}

func fnExec1(req *Request) *Response {
	res := &Response{Id: req.Id}

	resData := struct {
		ExitCode int
		Output   string
	}{}

	reqData := struct {
		Command string `json:"command"`
	}{}

	err := json.Unmarshal(req.RawArgs, &reqData)
	if err != nil {
		res.Error = &Error{Code: -1, Desc: err.Error()}
		return res
	}
	if reqData.Command == "" {
		res.Error = &Error{Code: -1, Desc: "empty command to guest-exec"}
		return res
	}
	cmdline, err := base64.StdEncoding.DecodeString(reqData.Command)
	if err != nil {
		res.Error = &Error{Code: -1, Desc: err.Error()}
		return res
	}

	output, err := exec.Command("sh", "-c", string(cmdline)).CombinedOutput()
	if err != nil {
		res.Error = &Error{Code: -1, Desc: err.Error()}
		return res
	}

	resData.Output = base64.StdEncoding.EncodeToString(output)
	resData.ExitCode = 0
	res.Return = resData
	return res
}

func fnExec2(req *Request) *Response {
	res := &Response{Id: req.Id}

	stdIn := bytes.NewBuffer(nil)
	stdOut := bytes.NewBuffer(nil)
	stdErr := bytes.NewBuffer(nil)

	resData := struct {
		Pid int `json:"pid"`
	}{}

	reqData := struct {
		Path   string `json:"path"`
		Args   string `json:"arg,omitempty"`
		Env    string `json:"env,omitempty"`
		Input  string `json:"input-data,omitempty"`
		Output bool   `json:"capture-output"`
	}{}

	err := json.Unmarshal(req.RawArgs, &reqData)
	if err != nil {
		res.Error = &Error{Code: -1, Desc: err.Error()}
		return res
	}
	if reqData.Path == "" {
		res.Error = &Error{Code: -1, Desc: "empty command to guest-exec"}
		return res
	}
	cmd := &exec.Cmd{
		Path: reqData.Path,
		Args: strings.Split(reqData.Args, " "),
		Env:  strings.Split(reqData.Env, " "),
		Dir:  "/",
		SysProcAttr: &syscall.SysProcAttr{
			Setsid: true,
		},
	}

	if reqData.Input != "" {
		inData, err := base64.StdEncoding.DecodeString(reqData.Input)
		if err != nil {
			res.Error = &Error{Code: -1, Desc: err.Error()}
			return res
		}
		stdIn.Write(inData)
		cmd.Stdin = stdIn
	}
	if reqData.Output {
		cmd.Stdout = stdOut
		cmd.Stderr = stdErr
	}

	if err = cmd.Start(); err != nil {
		res.Error = &Error{Code: -1, Desc: err.Error()}
		return res
	}

	execStatuses[cmd.Process.Pid] = &ExecStatus{
		Exited: false,
	}
	resData.Pid = cmd.Process.Pid
	res.Return = resData

	go fnExecWait(cmd, stdOut, stdErr)

	return res
}

func fnExecWait(cmd *exec.Cmd, stdOut *bytes.Buffer, stdErr *bytes.Buffer) {
	var code int

	s, ok := execStatuses[cmd.Process.Pid]
	if !ok {
		return
	}
	if err := cmd.Wait(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				code = status.ExitStatus()
			}
		}
	} else {
		code = 0
	}

	s.ExitCode = &code
	s.Exited = cmd.ProcessState.Exited()
	if stdOut.Len() > 16*1024*1024 {
		s.OutTrunc = true
		stdOut.Truncate(16 * 1024 * 1024)
	}
	s.OutData = base64.StdEncoding.EncodeToString(stdOut.Bytes())
	stdOut.Reset()
	if stdErr.Len() > 16*1024*1024 {
		s.ErrTrunc = true
		stdErr.Truncate(16 * 1024 * 1024)
	}
	s.ErrData = base64.StdEncoding.EncodeToString(stdErr.Bytes())
	stdErr.Reset()
}
