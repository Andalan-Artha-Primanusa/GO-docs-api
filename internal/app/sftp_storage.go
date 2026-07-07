package app

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func (a *App) newSFTPClient() (*sftp.Client, func(), error) {
	if a.cfg.SFTPHost == "" || a.cfg.SFTPUser == "" || a.cfg.SFTPPassword == "" {
		return nil, nil, fmt.Errorf("sftp config is incomplete")
	}
	port := a.cfg.SFTPPort
	if port == "" {
		port = "22"
	}
	sshClient, err := ssh.Dial("tcp", net.JoinHostPort(a.cfg.SFTPHost, port), &ssh.ClientConfig{
		User:            a.cfg.SFTPUser,
		Auth:            []ssh.AuthMethod{ssh.Password(a.cfg.SFTPPassword)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         20 * time.Second,
	})
	if err != nil {
		return nil, nil, err
	}
	client, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, nil, err
	}
	cleanup := func() {
		_ = client.Close()
		_ = sshClient.Close()
	}
	return client, cleanup, nil
}

func (a *App) sftpRoot() string {
	if a.cfg.SFTPDir == "" {
		return "/uploads"
	}
	return a.cfg.SFTPDir
}

func (a *App) saveUploadSFTP(src io.Reader, folder string, name string) error {
	client, cleanup, err := a.newSFTPClient()
	if err != nil {
		return err
	}
	defer cleanup()

	dir := path.Join(a.sftpRoot(), folder)
	if err := client.MkdirAll(dir); err != nil {
		return err
	}
	dst, err := client.Create(path.Join(dir, name))
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

func (a *App) serveUploadSFTP(w http.ResponseWriter, r *http.Request, rel string) error {
	cleanRel := path.Clean("/" + strings.ReplaceAll(rel, "\\", "/"))
	if cleanRel == "/" || strings.Contains(cleanRel, "/../") {
		return fmt.Errorf("invalid file path")
	}
	client, cleanup, err := a.newSFTPClient()
	if err != nil {
		return err
	}
	defer cleanup()

	remotePath := path.Join(a.sftpRoot(), strings.TrimPrefix(cleanRel, "/"))
	file, err := client.Open(remotePath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filepath.Base(remotePath)))
	http.ServeContent(w, r, filepath.Base(remotePath), info.ModTime(), file)
	return nil
}
