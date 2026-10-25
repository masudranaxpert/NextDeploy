package phphosting

import (
	"testing"
)

func BenchmarkDBMemoryDefaultMB(b *testing.B) {
	hosts := []int{512, 1024, 2048, 4096, 8192, 16384, 32768, 65536}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DBMemoryDefaultMB(hosts[i%len(hosts)])
	}
}

func BenchmarkTuneDB(b *testing.B) {
	mems := []int{256, 512, 1024, 2048, 4096, 8192}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = TuneDB(mems[i%len(mems)])
	}
}

func BenchmarkServerArgs(b *testing.B) {
	tn := TuneDB(2048)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tn.serverArgs()
	}
}

func BenchmarkUserLimitClause(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = userLimitClause("mariadb:11.4", 25, 60)
	}
}
