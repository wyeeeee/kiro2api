package config

import (
	"os"
	"testing"
)

func TestIsSaveRawDataEnabled(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     bool
	}{
		{
			name:     "未设置环境变量时返回false",
			envValue: "",
			want:     false,
		},
		{
			name:     "设置为true时返回true",
			envValue: "true",
			want:     true,
		},
		{
			name:     "设置为false时返回false",
			envValue: "false",
			want:     false,
		},
		{
			name:     "设置为其他值时返回false",
			envValue: "yes",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 保存原始环境变量
			oldValue := os.Getenv("SAVE_RAW_DATA")
			defer os.Setenv("SAVE_RAW_DATA", oldValue)

			// 设置测试环境变量
			if tt.envValue == "" {
				os.Unsetenv("SAVE_RAW_DATA")
			} else {
				os.Setenv("SAVE_RAW_DATA", tt.envValue)
			}

			// 测试
			got := IsSaveRawDataEnabled()
			if got != tt.want {
				t.Errorf("IsSaveRawDataEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
