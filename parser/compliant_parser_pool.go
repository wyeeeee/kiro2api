package parser

import (
	"sync"
)

// CompliantParserPool 符合AWS规范的事件流解析器池
type CompliantParserPool struct {
	pool sync.Pool
}

// 全局符合规范的解析器池
var GlobalCompliantParserPool = &CompliantParserPool{
	pool: sync.Pool{
		New: func() any {
			return NewCompliantEventStreamParser(false) // 默认非严格模式，允许部分错误
		},
	},
}

// Get 从对象池获取符合规范的解析器实例
func (cpp *CompliantParserPool) Get() *CompliantEventStreamParser {
	return cpp.pool.Get().(*CompliantEventStreamParser)
}

// Put 将解析器实例放回对象池
func (cpp *CompliantParserPool) Put(parser *CompliantEventStreamParser) {
	// 重置解析器状态以供重用
	parser.Reset()
	cpp.pool.Put(parser)
}

// SetStrictMode 设置池中所有新创建解析器的严格模式
func (cpp *CompliantParserPool) SetStrictMode(strict bool) {
	// 更新池的创建函数
	cpp.pool = sync.Pool{
		New: func() any {
			return NewCompliantEventStreamParser(strict)
		},
	}
}
