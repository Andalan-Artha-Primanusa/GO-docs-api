package app

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ftpClient struct {
	conn   net.Conn
	reader *bufio.Reader
}

func (a *App) newFTPClient() (*ftpClient, error) {
	if a.cfg.FTPHost == "" || a.cfg.FTPUser == "" || a.cfg.FTPPassword == "" {
		return nil, fmt.Errorf("ftp config is incomplete")
	}
	if a.cfg.FTPTLS || a.cfg.UploadStorage == "ftps" {
		return nil, fmt.Errorf("ftps is not supported by this build; set FTP_TLS=false and enable FTP on NAS")
	}
	port := a.cfg.FTPPort
	if port == "" {
		port = "21"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(a.cfg.FTPHost, port), 20*time.Second)
	if err != nil {
		return nil, err
	}
	client := &ftpClient{conn: conn, reader: bufio.NewReader(conn)}
	if _, _, err := client.readResponse(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if code, msg, err := client.command("USER %s", a.cfg.FTPUser); err != nil {
		_ = conn.Close()
		return nil, err
	} else if code == 331 {
		if code, msg, err = client.command("PASS %s", a.cfg.FTPPassword); err != nil || code >= 400 {
			_ = conn.Close()
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("ftp login failed: %s", msg)
		}
	} else if code >= 400 {
		_ = conn.Close()
		return nil, fmt.Errorf("ftp login failed: %s", msg)
	}
	if code, msg, err := client.command("TYPE I"); err != nil || code >= 400 {
		_ = conn.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("ftp binary mode failed: %s", msg)
	}
	return client, nil
}

func (c *ftpClient) close() {
	_, _, _ = c.command("QUIT")
	_ = c.conn.Close()
}

func (c *ftpClient) command(format string, args ...any) (int, string, error) {
	if _, err := fmt.Fprintf(c.conn, format+"\r\n", args...); err != nil {
		return 0, "", err
	}
	return c.readResponse()
}

func (c *ftpClient) readResponse() (int, string, error) {
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return 0, "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) < 3 {
		return 0, line, fmt.Errorf("invalid ftp response: %s", line)
	}
	code, err := strconv.Atoi(line[:3])
	if err != nil {
		return 0, line, err
	}
	if len(line) > 3 && line[3] == '-' {
		prefix := line[:3] + " "
		for {
			next, err := c.reader.ReadString('\n')
			if err != nil {
				return 0, line, err
			}
			next = strings.TrimRight(next, "\r\n")
			line += "\n" + next
			if strings.HasPrefix(next, prefix) {
				break
			}
		}
	}
	return code, line, nil
}

func (c *ftpClient) passiveConn() (net.Conn, error) {
	code, msg, err := c.command("PASV")
	if err != nil {
		return nil, err
	}
	if code != 227 {
		return nil, fmt.Errorf("ftp pasv failed: %s", msg)
	}
	start := strings.Index(msg, "(")
	end := strings.Index(msg, ")")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("invalid pasv response: %s", msg)
	}
	parts := strings.Split(msg[start+1:end], ",")
	if len(parts) != 6 {
		return nil, fmt.Errorf("invalid pasv address: %s", msg)
	}
	nums := make([]int, 6)
	for i, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		nums[i] = value
	}
	host := fmt.Sprintf("%d.%d.%d.%d", nums[0], nums[1], nums[2], nums[3])
	port := nums[4]*256 + nums[5]
	return net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 20*time.Second)
}

func (a *App) ftpRoot() string {
	if a.cfg.FTPDir == "" {
		return "/uploads"
	}
	return a.cfg.FTPDir
}

func (a *App) saveUploadFTP(src io.Reader, folder string, name string) error {
	client, err := a.newFTPClient()
	if err != nil {
		return err
	}
	defer client.close()

	dir := path.Join(a.ftpRoot(), folder)
	if err := makeFTPDirAll(client, dir); err != nil {
		return err
	}
	dataConn, err := client.passiveConn()
	if err != nil {
		return err
	}
	remotePath := path.Join(dir, name)
	if _, _, err := client.command("STOR %s", remotePath); err != nil {
		_ = dataConn.Close()
		return err
	}
	if _, err := io.Copy(dataConn, src); err != nil {
		_ = dataConn.Close()
		return err
	}
	if err := dataConn.Close(); err != nil {
		return err
	}
	code, msg, err := client.readResponse()
	if err != nil {
		return err
	}
	if code >= 400 {
		return fmt.Errorf("ftp upload failed: %s", msg)
	}
	return nil
}

func (a *App) serveUploadFTP(w http.ResponseWriter, r *http.Request, rel string) error {
	cleanRel := path.Clean("/" + strings.ReplaceAll(rel, "\\", "/"))
	if cleanRel == "/" || strings.Contains(cleanRel, "/../") {
		return fmt.Errorf("invalid file path")
	}
	client, err := a.newFTPClient()
	if err != nil {
		return err
	}
	defer client.close()

	remotePath := path.Join(a.ftpRoot(), strings.TrimPrefix(cleanRel, "/"))
	dataConn, err := client.passiveConn()
	if err != nil {
		return err
	}
	if _, _, err := client.command("RETR %s", remotePath); err != nil {
		_ = dataConn.Close()
		return err
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, dataConn); err != nil {
		_ = dataConn.Close()
		return err
	}
	if err := dataConn.Close(); err != nil {
		return err
	}
	code, msg, err := client.readResponse()
	if err != nil {
		return err
	}
	if code >= 400 {
		return fmt.Errorf("ftp download failed: %s", msg)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filepath.Base(remotePath)))
	http.ServeContent(w, r, filepath.Base(remotePath), time.Now(), bytes.NewReader(buf.Bytes()))
	return nil
}

func makeFTPDirAll(client *ftpClient, dir string) error {
	clean := path.Clean("/" + strings.TrimPrefix(dir, "/"))
	current := ""
	for _, part := range strings.Split(strings.TrimPrefix(clean, "/"), "/") {
		if part == "" {
			continue
		}
		current = path.Join(current, part)
		code, msg, err := client.command("MKD %s", "/"+current)
		if err != nil {
			return err
		}
		if code >= 400 && !strings.Contains(msg, "550") {
			return fmt.Errorf("ftp mkdir failed: %s", msg)
		}
	}
	return nil
}
