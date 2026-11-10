package gameserver

import (
	"context"
	"testing"
)

// 扫频列表只做排查用，但它决定结算界面出几张卡，解析错会让玩家看到一堆莫名其妙的
// 礼物卡，所以把边界（空值、非法项、上限 6）都锁住。
func TestWaterwayTrophyGiftSweepParse(t *testing.T) {
	t.Parallel()
	s, db := newDetachTestServer(t)
	ctx := context.Background()
	if err := db.TunableSet(ctx, waterwayTrophyGiftSweepKey, " 6, 4 ,x,999,0,3 ", "test"); err != nil {
		t.Fatal(err)
	}
	got := s.waterwayTrophyGiftSweep()
	want := []int{6, 4, 3}
	if len(got) != len(want) {
		t.Fatalf("扫频解析 = %v，期望 %v（非法项/越界值应被丢弃）", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("扫频解析 = %v，期望 %v", got, want)
		}
	}
}

func TestWaterwayTrophyGiftSweepEmpty(t *testing.T) {
	t.Parallel()
	s, _ := newDetachTestServer(t)
	if got := s.waterwayTrophyGiftSweep(); got != nil {
		t.Fatalf("未配置扫频时不该有候选，实得 %v", got)
	}
}

func TestWaterwayTrophyGiftSweepCap(t *testing.T) {
	t.Parallel()
	s, db := newDetachTestServer(t)
	ctx := context.Background()
	if err := db.TunableSet(ctx, waterwayTrophyGiftSweepKey, "1,2,3,4,5,6,7,8", "test"); err != nil {
		t.Fatal(err)
	}
	if got := s.waterwayTrophyGiftSweep(); len(got) != 6 {
		t.Fatalf("扫频最多 6 个候选，实得 %d 个：%v", len(got), got)
	}
}
