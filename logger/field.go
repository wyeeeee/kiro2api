package logger

import (
	"fmt"
	"time"
)

// Field 结构化字段类型
type Field struct {
	Key   string
	Value interface{}
	Type  FieldType
}

// FieldType 字段类型枚举
type FieldType int

const (
	StringType FieldType = iota
	IntType
	FloatType
	BoolType
	DurationType
	TimeType
	ErrorType
)

// String 创建字符串类型字段
func String(key, value string) Field {
	return Field{
		Key:   key,
		Value: value,
		Type:  StringType,
	}
}

// Int 创建整数类型字段
func Int(key string, value int) Field {
	return Field{
		Key:   key,
		Value: value,
		Type:  IntType,
	}
}

// Int64 创建int64类型字段
func Int64(key string, value int64) Field {
	return Field{
		Key:   key,
		Value: value,
		Type:  IntType,
	}
}

// Float64 创建浮点数类型字段
func Float64(key string, value float64) Field {
	return Field{
		Key:   key,
		Value: value,
		Type:  FloatType,
	}
}

// Bool 创建布尔类型字段
func Bool(key string, value bool) Field {
	return Field{
		Key:   key,
		Value: value,
		Type:  BoolType,
	}
}

// Duration 创建时间间隔类型字段
func Duration(key string, value time.Duration) Field {
	return Field{
		Key:   key,
		Value: value,
		Type:  DurationType,
	}
}

// Time 创建时间类型字段
func Time(key string, value time.Time) Field {
	return Field{
		Key:   key,
		Value: value,
		Type:  TimeType,
	}
}

// Err 创建错误类型字段
func Err(err error) Field {
	return Field{
		Key:   "error",
		Value: err.Error(),
		Type:  ErrorType,
	}
}

// Any 创建任意类型字段
func Any(key string, value interface{}) Field {
	return Field{
		Key:   key,
		Value: value,
		Type:  StringType, // 默认当作字符串处理
	}
}

// FormatValue 格式化字段值为字符串
func (f Field) FormatValue() string {
	switch f.Type {
	case StringType:
		return fmt.Sprintf("%s", f.Value)
	case IntType:
		return fmt.Sprintf("%d", f.Value)
	case FloatType:
		return fmt.Sprintf("%g", f.Value)
	case BoolType:
		return fmt.Sprintf("%t", f.Value)
	case DurationType:
		if dur, ok := f.Value.(time.Duration); ok {
			return dur.String()
		}
		return fmt.Sprintf("%v", f.Value)
	case TimeType:
		if t, ok := f.Value.(time.Time); ok {
			return t.Format(time.RFC3339)
		}
		return fmt.Sprintf("%v", f.Value)
	case ErrorType:
		return fmt.Sprintf("%s", f.Value)
	default:
		return fmt.Sprintf("%v", f.Value)
	}
}
