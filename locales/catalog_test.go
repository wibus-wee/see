package locales

import "testing"

func TestChineseMessagesAndPlurals(t *testing.T) {
	zh := In("zh-Hans")
	if got := zh.Language(); got != "语言" {
		t.Fatalf("language = %q", got)
	}
	if got := zh.MsgDFileSChanged(2); got != "已更改 2 个文件" {
		t.Fatalf("Chinese count = %q", got)
	}
	en := In("en")
	if got := en.MsgDFileSChanged(1); got != "1 file changed" {
		t.Fatalf("English singular = %q", got)
	}
	if got := en.MsgDFileSChanged(2); got != "2 files changed" {
		t.Fatalf("English plural = %q", got)
	}
}
