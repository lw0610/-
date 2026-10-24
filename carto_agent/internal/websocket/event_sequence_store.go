package websocket

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/logger"
)

func sequencePath(sessionID, projectPath string) (string, error) {
	dir, err := config.ResolveSessionDir(projectPath, sessionID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".run-event-seq"), nil
}
func readEventSequence(path string) (uint64, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() || info.Size() != 8 {
		return 0, fmt.Errorf("运行事件序号文件无效")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(raw), nil
}
func writeEventSequence(path string, seq uint64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".event-seq-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], seq)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(raw[:])
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}
func (s *DeepServer) seedSessionEventSequence(sessionID, projectPath string) {
	path, err := sequencePath(sessionID, projectPath)
	if err != nil {
		return
	}
	s.eventSequenceMu.Lock()
	defer s.eventSequenceMu.Unlock()
	saved, err := readEventSequence(path)
	if err != nil {
		logger.Warn("恢复事件序号失败: session_id=%s err=%v", sessionID, err)
		return
	}
	s.mu.Lock()
	if s.sessionRunEventSeq == nil {
		s.sessionRunEventSeq = map[string]uint64{}
	}
	if saved > s.sessionRunEventSeq[sessionID] {
		s.sessionRunEventSeq[sessionID] = saved
	}
	s.mu.Unlock()
}
