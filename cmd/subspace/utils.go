package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"text/template"
	"time"
)

func RandomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.URLEncoding.EncodeToString(b)[:n]
}

func Overwrite(filename string, data []byte, perm os.FileMode) error {
	f, err := ioutil.TempFile(filepath.Dir(filename), filepath.Base(filename)+".tmp")
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(f.Name(), perm); err != nil {
		return err
	}
	return os.Rename(f.Name(), filename)
}

func bash(tmpl string, params interface{}) (string, error) {
	preamble := `
set -o nounset
set -o errexit
set -o pipefail
set -o xtrace
`
	t, err := template.New("template").Parse(preamble + tmpl)
	if err != nil {
		return "", err
	}
	var script bytes.Buffer
	err = t.Execute(&script, params)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	output, err := exec.CommandContext(ctx, "/bin/bash", "-c", string(script.Bytes())).CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("command failed: %s\n%s", err, string(output))
	}
	return string(output), nil
}

func logErrorAndRedirect(err error, w *Web, file string, redirectTo string) {
	logger.Warn(err)
	f, _ := os.Create(file)
	errstr := fmt.Sprintln(err)
	f.WriteString(errstr)
	w.Redirect(redirectTo)
}

type MutexBash struct {
	m sync.Mutex
}

func (mb *MutexBash) Bash(tmpl string, params interface{}) (string, error) {
	mb.m.Lock()
	defer mb.m.Unlock()
	return bash(tmpl, params)
}

type ServerConfig struct {
	mb MutexBash
}

func (sc *ServerConfig) Update() (string, error) {
	script := `
cd {{$.Datadir}}/wireguard

cat <<WGSERVER >server.conf
[Interface]
PrivateKey = $(cat server.private)
ListenPort = ${SUBSPACE_LISTENPORT}

WGSERVER
cat peers/*.conf >>server.conf
`
	return sc.mb.Bash(script, struct {
		Datadir string
	}{
		datadir,
	})
}
