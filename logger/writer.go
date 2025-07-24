package logger

import (
	"bufio"
	"fmt"
	"os"
	"sync"
)

// Writer 输出器接口
type Writer interface {
	Write(data []byte) error
	Close() error
}

// ConsoleWriter 控制台输出器
type ConsoleWriter struct{}

// NewConsoleWriter 创建控制台输出器
func NewConsoleWriter() *ConsoleWriter {
	return &ConsoleWriter{}
}

// Write 写入数据到控制台
func (w *ConsoleWriter) Write(data []byte) error {
	_, err := os.Stdout.Write(data)
	return err
}

// Close 关闭控制台输出器（无操作）
func (w *ConsoleWriter) Close() error {
	return nil
}

// FileWriter 文件输出器
type FileWriter struct {
	filename string
	file     *os.File
	buffer   *bufio.Writer
	mutex    sync.Mutex
}

// NewFileWriter 创建文件输出器
func NewFileWriter(filename string) (*FileWriter, error) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %s: %w", filename, err)
	}

	buffer := bufio.NewWriter(file)

	return &FileWriter{
		filename: filename,
		file:     file,
		buffer:   buffer,
	}, nil
}

// Write 写入数据到文件
func (w *FileWriter) Write(data []byte) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.buffer == nil {
		return fmt.Errorf("file writer is closed")
	}

	_, err := w.buffer.Write(data)
	if err != nil {
		return err
	}

	// 立即刷新缓冲区以确保数据写入
	return w.buffer.Flush()
}

// Close 关闭文件输出器
func (w *FileWriter) Close() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.buffer != nil {
		w.buffer.Flush()
		w.buffer = nil
	}

	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}

	return nil
}

// MultiWriter 多路输出器
type MultiWriter struct {
	writers []Writer
	mutex   sync.RWMutex
}

// NewMultiWriter 创建多路输出器
func NewMultiWriter(writers ...Writer) *MultiWriter {
	return &MultiWriter{
		writers: writers,
	}
}

// Write 写入数据到所有输出器
func (w *MultiWriter) Write(data []byte) error {
	w.mutex.RLock()
	defer w.mutex.RUnlock()

	for _, writer := range w.writers {
		if err := writer.Write(data); err != nil {
			// 记录错误但继续写入其他输出器
			fmt.Fprintf(os.Stderr, "Logger write error: %v\n", err)
		}
	}
	return nil
}

// Close 关闭所有输出器
func (w *MultiWriter) Close() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	var lastErr error
	for _, writer := range w.writers {
		if err := writer.Close(); err != nil {
			lastErr = err
		}
	}

	w.writers = nil
	return lastErr
}

// AddWriter 添加输出器
func (w *MultiWriter) AddWriter(writer Writer) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	w.writers = append(w.writers, writer)
}

// RemoveWriter 移除输出器
func (w *MultiWriter) RemoveWriter(writer Writer) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	for i, wr := range w.writers {
		if wr == writer {
			w.writers = append(w.writers[:i], w.writers[i+1:]...)
			break
		}
	}
}
