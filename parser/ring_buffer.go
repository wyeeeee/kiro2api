package parser

import (
	"errors"
)

var (
	ErrBufferFull  = errors.New("ring buffer is full")
	ErrBufferEmpty = errors.New("ring buffer is empty")
)

// RingBuffer 环形缓冲区实现
type RingBuffer struct {
	data  []byte
	size  int
	head  int // 读取位置
	tail  int // 写入位置
	count int // 当前数据量
	// 移除所有锁 - 单goroutine独占使用时不需要并发保护
}

// NewRingBuffer 创建新的环形缓冲区
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data:  make([]byte, size),
		size:  size,
		head:  0,
		tail:  0,
		count: 0,
		// 无锁设计 - 移除条件变量和互斥锁
	}
}

// Write 写入数据到环形缓冲区
func (rb *RingBuffer) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	written := 0
	for len(data) > 0 {
		// 检查可用空间
		available := rb.availableSpace()
		if available == 0 {
			return written, ErrBufferFull // 无锁设计：直接返回而不等待
		}

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
	}

	return written, nil
}

// Read 从环形缓冲区读取数据
func (rb *RingBuffer) Read(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}

	// 检查可用数据
	available := rb.availableData()
	if available == 0 {
		return 0, ErrBufferEmpty // 无锁设计：直接返回而不等待
	}

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

	return toRead, nil
}

// TryRead 非阻塞读取
func (rb *RingBuffer) TryRead(buf []byte) (int, error) {

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

	return toRead, nil
}

// TryWrite 非阻塞写入
func (rb *RingBuffer) TryWrite(data []byte) (int, error) {

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

	return toWrite, nil
}

// Peek 查看数据但不移除
func (rb *RingBuffer) Peek(buf []byte) (int, error) {

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

	available := rb.availableData()
	toSkip := min(available, n)

	rb.head = (rb.head + toSkip) % rb.size
	rb.count -= toSkip

	return toSkip
}

// Available 返回可读取的字节数
func (rb *RingBuffer) Available() int {
	return rb.count
}

// Free 返回可写入的字节数
func (rb *RingBuffer) Free() int {
	return rb.size - rb.count
}

// Reset 重置缓冲区
func (rb *RingBuffer) Reset() {
	// 无锁设计：直接重置状态
	rb.head = 0
	rb.tail = 0
	rb.count = 0
}

// IsFull 检查缓冲区是否已满
func (rb *RingBuffer) IsFull() bool {
	return rb.isFull()
}

// IsEmpty 检查缓冲区是否为空
func (rb *RingBuffer) IsEmpty() bool {
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
