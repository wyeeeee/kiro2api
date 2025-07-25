package utils

import "io"

// ReadHTTPResponse 通用的HTTP响应体读取函数
func ReadHTTPResponse(body io.Reader) ([]byte, error) {
	result := make([]byte, 0)
	buf := make([]byte, 1024)

	for {
		n, err := body.Read(buf)
		if n > 0 {
			result = append(result, buf[:n]...)
		}
		if err != nil {
			if err == io.EOF {
				return result, nil
			}
			return result, err
		}
	}
}
