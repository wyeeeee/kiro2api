package parser

import (
	"errors"
	"sync"
)

var (
	ErrBufferFull  = errors.New("ring buffer is full")
	ErrBufferEmpty = errors.New("ring buffer is empty")
)

// RingBuffer 环形缓冲区实现
type RingBuffer struct {
	data     []byte
	size     int
	head     int // 读取位置
	tail     int // 写入位置
	count    int // 当前数据量
	mu       sync.RWMutex
	notEmpty *sync.Cond
	notFull  *sync.Cond
}

// NewRingBuffer 创建新的环形缓冲区
func NewRingBuffer(size int) *RingBuffer {
	rb := &RingBuffer{
		data:  make([]byte, size),
		size:  size,
		head:  0,
		tail:  0,
		count: 0,
	}
	rb.notEmpty = sync.NewCond(&rb.mu)
	rb.notFull = sync.NewCond(&rb.mu)
	return rb
}

// Write 写入数据到环形缓冲区
func (rb *RingBuffer) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	written := 0
	for len(data) > 0 {
		// 等待有可用空间
		for rb.isFull() {
			rb.notFull.Wait()
		}

		// 计算可写入的字节数
		available := rb.availableSpace()
		toWrite := min(available, len(data))

		// 分两段写入（如果需要环绕）
		if rb.tail+toWrite <= rb.size {
			// 不需要环绕，直接写入
			copy(rb.data[rb.tail:rb.tail+toWrite], data[:toWrite])
		} else {
			// 需要环绕，分两段写入
			firstPart := rb.size - rb.tail
			copy(rb.data[rb.tail:], data[:firstPart])
			copy(rb.data[0:], data[firstPart:toWrite])
		}

		rb.tail = (rb.tail + toWrite) % rb.size
		rb.count += toWrite
		written += toWrite
		data = data[toWrite:]

		// 通知有数据可读
		rb.notEmpty.Signal()
	}

	return written, nil
}

// Read 从环形缓冲区读取数据
func (rb *RingBuffer) Read(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	// 等待有数据可读
	for rb.isEmpty() {
		rb.notEmpty.Wait()
	}

	// 计算可读取的字节数
	available := rb.availableData()
	toRead := min(available, len(buf))

	// 分两段读取（如果需要环绕）
	if rb.head+toRead <= rb.size {
		// 不需要环绕，直接读取
		copy(buf[:toRead], rb.data[rb.head:rb.head+toRead])
	} else {
		// 需要环绕，分两段读取
		firstPart := rb.size - rb.head
		copy(buf[:firstPart], rb.data[rb.head:])
		copy(buf[firstPart:toRead], rb.data[0:])
	}

	rb.head = (rb.head + toRead) % rb.size
	rb.count -= toRead

	// 通知有空间可写
	rb.notFull.Signal()

	return toRead, nil
}

// TryRead 非阻塞读取
func (rb *RingBuffer) TryRead(buf []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.isEmpty() {
		return 0, ErrBufferEmpty
	}

	available := rb.availableData()
	toRead := min(available, len(buf))

	if rb.head+toRead <= rb.size {
		copy(buf[:toRead], rb.data[rb.head:rb.head+toRead])
	} else {
		firstPart := rb.size - rb.head
		copy(buf[:firstPart], rb.data[rb.head:])
		copy(buf[firstPart:toRead], rb.data[0:])
	}

	rb.head = (rb.head + toRead) % rb.size
	rb.count -= toRead
	rb.notFull.Signal()

	return toRead, nil
}

// TryWrite 非阻塞写入
func (rb *RingBuffer) TryWrite(data []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.isFull() {
		return 0, ErrBufferFull
	}

	available := rb.availableSpace()
	toWrite := min(available, len(data))

	if rb.tail+toWrite <= rb.size {
		copy(rb.data[rb.tail:rb.tail+toWrite], data[:toWrite])
	} else {
		firstPart := rb.size - rb.tail
		copy(rb.data[rb.tail:], data[:firstPart])
		copy(rb.data[0:], data[firstPart:toWrite])
	}

	rb.tail = (rb.tail + toWrite) % rb.size
	rb.count += toWrite
	rb.notEmpty.Signal()

	return toWrite, nil
}

// Peek 查看数据但不移除
func (rb *RingBuffer) Peek(buf []byte) (int, error) {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.isEmpty() {
		return 0, ErrBufferEmpty
	}

	available := rb.availableData()
	toRead := min(available, len(buf))

	if rb.head+toRead <= rb.size {
		copy(buf[:toRead], rb.data[rb.head:rb.head+toRead])
	} else {
		firstPart := rb.size - rb.head
		copy(buf[:firstPart], rb.data[rb.head:])
		copy(buf[firstPart:toRead], rb.data[0:])
	}

	return toRead, nil
}

// Skip 跳过指定字节数
func (rb *RingBuffer) Skip(n int) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	available := rb.availableData()
	toSkip := min(available, n)

	rb.head = (rb.head + toSkip) % rb.size
	rb.count -= toSkip
	rb.notFull.Signal()

	return toSkip
}

// Available 返回可读取的字节数
func (rb *RingBuffer) Available() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// Free 返回可写入的字节数
func (rb *RingBuffer) Free() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.size - rb.count
}

// Reset 重置缓冲区
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.head = 0
	rb.tail = 0
	rb.count = 0
}

// IsFull 检查缓冲区是否已满
func (rb *RingBuffer) IsFull() bool {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.isFull()
}

// IsEmpty 检查缓冲区是否为空
func (rb *RingBuffer) IsEmpty() bool {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.isEmpty()
}

// 内部辅助方法（不加锁）

func (rb *RingBuffer) isFull() bool {
	return rb.count == rb.size
}

func (rb *RingBuffer) isEmpty() bool {
	return rb.count == 0
}

func (rb *RingBuffer) availableSpace() int {
	return rb.size - rb.count
}

func (rb *RingBuffer) availableData() int {
	return rb.count
}

// min函数已在其他文件中定义
