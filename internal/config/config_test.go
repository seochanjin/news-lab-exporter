package config

import "testing"

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		fallback int
		want     int
		wantErr  bool
	}{
		{name: "값이 없으면 기본값을 쓴다", envValue: "", fallback: 4, want: 4},
		{name: "정수 문자열을 파싱한다", envValue: "8", fallback: 4, want: 8},
		{name: "정수가 아니면 오류", envValue: "abc", fallback: 4, wantErr: true},
		{name: "0이면 오류", envValue: "0", fallback: 4, wantErr: true},
		{name: "음수면 오류", envValue: "-1", fallback: 4, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DB_MAX_CONNS", tt.envValue)

			got, err := getEnvInt("DB_MAX_CONNS", tt.fallback)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("오류를 기대했으나 nil이 반환됐다 (got=%d)", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("예상치 못한 오류: %v", err)
			}

			if got != tt.want {
				t.Errorf("got=%d, want=%d", got, tt.want)
			}
		})
	}
}

func TestLoad_DATABASE_URL이_없으면_실패한다(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	_, err := Load()
	if err == nil {
		t.Fatal("DATABASE_URL이 없으면 오류여야 한다")
	}
}
