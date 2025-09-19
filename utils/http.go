package utils

import "io"

// ReadHTTPResponse 通用的HTTP响应体读取函数（使用对象池优化）
func ReadHTTPResponse(body io.Reader) ([]byte, error) {
	// 使用对象池获取缓冲区，避免频繁内存分配
	buffer := GetBuffer()
	defer PutBuffer(buffer)

	// 使用对象池获取读取缓冲区
	buf := GetByteSlice()
	defer PutByteSlice(buf)
	buf = buf[:1024] // 限制为1024字节

	for {
		n, err := body.Read(buf)
		if n > 0 {
			buffer.Write(buf[:n])
		}
		if err != nil {
			if err == io.EOF {
				// 复制结果并返回，因为buffer会被回收
				result := make([]byte, buffer.Len())
				copy(result, buffer.Bytes())
				return result, nil
			}
			// 复制结果并返回，因为buffer会被回收
			result := make([]byte, buffer.Len())
			copy(result, buffer.Bytes())
			return result, err
		}
	}
}
