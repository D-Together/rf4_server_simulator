package gameserver

import (
	"math"
	"testing"

	"rf4server/internal/proto"
)

// TestFightCurvesKeepSprintTensionStages 验证曲线池是官方口径的纯 uniform 缩放：
// 全部行 = 池原值 × (pull/池基准)，张力包络即池自身包络（crucian 池 1.414..3.677，
// 基准 2.828 → 0.50×pull..1.30×pull；官方 crucian 293g 实测 min≈0.49×pull）。
func TestFightCurvesKeepSprintTensionStages(t *testing.T) {
	srv, _ := newRigTestServer(t)
	const pull = 50.0
	curves := srv.buildFightCurves("crucian", pull)
	if curves == nil || len(curves.Items) < 20 {
		t.Fatalf("搏鱼曲线行数=%d，期望完整冲刺阶段池", len(curves.Items))
	}

	stageIDs := map[uint32]bool{}
	minTension, maxTension := math.Inf(1), 0.0
	for _, row := range curves.Items {
		stageField := row.Field(0)
		stage := stageField.U32()
		stageIDs[stage] = true
		for _, idx := range []int{8, 9} {
			v := protoF32Field(row, idx)
			if v <= 0 {
				t.Fatalf("阶段 %d 的 f%d 张力无效: %f", stage, idx, v)
			}
			minTension = math.Min(minTension, v)
			maxTension = math.Max(maxTension, v)
		}
	}
	for _, stage := range []uint32{104, 105, 301, 302, 306} {
		if !stageIDs[stage] {
			t.Errorf("缺少冲刺/恢复阶段 %d", stage)
		}
	}
	// uniform 缩放：min/max 比例固定为池包络，与 pull 无关。
	if minTension < pull*0.45 || maxTension > pull*1.35 {
		t.Fatalf("曲线张力范围 %.2f..%.2f 偏离 crucian 池 uniform 包络（基准 %.2f）",
			minTension, maxTension, pull)
	}
	if minTension >= pull || maxTension <= pull {
		t.Fatalf("张力包络未跨过基准拉力 %.2f，冲刺差异丢失", pull)
	}
}

// TestFishPullScalesWithSpeciesAndWeight 验证 f8 的官方规律：
// 同鱼种体重大→拉力大（ln 斜率）；掠食者 K0 高于静水小鲤；海水鳕科最高。
func TestFishPullScalesWithSpeciesAndWeight(t *testing.T) {
	srv, _ := newRigTestServer(t)
	pikeSmall := srv.fishFightPullFor(&RolledFish{Species: "n.pike", WeightG: 150})
	pikeLarge := srv.fishFightPullFor(&RolledFish{Species: "n.pike", WeightG: 8000})
	roachPull := srv.fishFightPullFor(&RolledFish{Species: "c.roach", WeightG: 1000})
	crucianPull := srv.fishFightPullFor(&RolledFish{Species: "crucian", WeightG: 1000})

	if pikeLarge <= pikeSmall {
		t.Fatalf("同鱼种大体型拉力应更强（ln 斜率）: small=%f large=%f", pikeSmall, pikeLarge)
	}
	if roachPull <= crucianPull {
		t.Fatalf("拟鲤族 K0 应高于静水小鲤: roach=%f crucian=%f", roachPull, crucianPull)
	}
	// 同重量（1kg，ln 项归零）下比 K0：海水鳕科 > 掠食者 > 拟鲤族 > 静水小鲤。
	pikeK1 := srv.fishFightPullFor(&RolledFish{Species: "n.pike", WeightG: 1000})
	merlangK1 := srv.fishFightPullFor(&RolledFish{Species: "merlang", WeightG: 1000})
	if merlangK1 <= pikeK1 {
		t.Fatalf("海水鳕科 K0 应高于掠食者: merlang(1kg)=%f pike(1kg)=%f", merlangK1, pikeK1)
	}

	// 未实测鱼种走习性组/fallback，不得为 0 或负。
	fallback := srv.fishFightPullFor(&RolledFish{Species: "not_in_calib", WeightG: 500})
	if fallback <= 0 {
		t.Fatalf("未测种拉力=%f，应走组表/fallback", fallback)
	}
}

// TestFightCurvesUniformEnvelope 验证同一池不同拉力下曲线是纯 uniform：
// 每行张力 / pull 恒等于池原比例（官方逐行精确成立的规律）。
func TestFightCurvesUniformEnvelope(t *testing.T) {
	srv, _ := newRigTestServer(t)
	ratio := func(pull float64) []float64 {
		curves := srv.buildFightCurves("crucian", pull)
		var out []float64
		for _, row := range curves.Items {
			out = append(out, protoF32Field(row, 8)/pull)
		}
		return out
	}
	a, b := ratio(10), ratio(80)
	if len(a) != len(b) || len(a) == 0 {
		t.Fatalf("曲线行数不一致: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-4 {
			t.Fatalf("行 %d 非 uniform 缩放: %f vs %f", i, a[i], b[i])
		}
	}
}

func protoF32Field(row *proto.Value, idx int) float64 {
	if row == nil || idx < 0 || idx >= len(row.Fields) || row.Fields[idx].Num == nil {
		return 0
	}
	return float64(proto.F32FromBits(uint32(u64Of(row.Fields[idx].Num))))
}
