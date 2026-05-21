package cmd

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

func copyExistingBrainJSONL(srcDir, dstDir, filename string) (string, int, error) {
	srcPath := filepath.Join(srcDir, filename)
	in, err := os.Open(srcPath) //nolint:gosec
	if err != nil {
		if os.IsNotExist(err) {
			return writeBrainJSONL(dstDir, filename, nil)
		}
		return "", 0, err
	}
	defer func() { _ = in.Close() }()

	dstPath := filepath.Join(dstDir, filename)
	out, err := os.Create(dstPath) //nolint:gosec
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	count := 0
	reader := bufio.NewReader(in)
	writer := bufio.NewWriter(out)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, writeErr := writer.Write(line); writeErr != nil {
				_ = out.Close()
				return "", 0, writeErr
			}
			h.Write(line)
			count++
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			_ = out.Close()
			return "", 0, readErr
		}
	}
	if err := writer.Flush(); err != nil {
		_ = out.Close()
		return "", 0, err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return "", 0, err
	}
	if err := out.Close(); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), count, nil
}
