package gameserver

// 「渔夫稀有鱼」面板（3/17）的**收录清单**。
//
// 这张面板不是「该水域能钓到的全部鱼种」。官方给每个水域单独编了一份表，只收十来个
// 到三四十个鱼种，面板上就这几格（没钓过的显示灰色「未钓到」），集齐它们才算完成该
// 水域的渔夫图鉴，也才发该水域的奖杯。
//
// 这份表只存在于客户端：服务端静态库（loc_fish / loc_wiki_fish / map_fish_roster）描述
// 的是「这张图能钓到什么」，全都比面板大一圈（惟有诺克河 20 种 vs 面板 10 格，白河 19
// vs 10，阿赫图巴 54 vs 37）。拿它们当分母永远不会集齐——这就是「面板看着满了却拿不
// 到奖杯」的根因。
//
// 清单来源：玩家逐图截图反推（2026-11-10 的 18 张，外加 2026-11-11 补录的埃尔克湖）。反推方法：面板卡片显示的
// 是玩家自己的纪录，用**克重+体长**回查 catch_log 即可锁定 fish_key；对不上重量的
// （未钓到）按该水域鱼种池 + 客户端的显示名比对。注意客户端显示名与 loc_fish.name_zh
// 并不总一致（常见鲃鱼=c_barbel、长吻梅花鲈=ruffe_n、河螯虾=crawfish、鳟鱼=
// riv_brown_trout、莱芒湖白鲑=sig_ludoga、拉多加白鲑=ripus、欧白鲑=vendace、
// 湖鳟鱼=gc_smelt、红鳟=riv_rb_trout、蓝舒鳣=molva、魣鳕=m_molva），不能只按名字猜。
//
// key 是水域 systemID（loc_alias.alias），value 是面板格子（鱼种 key + 客户端显示名）。
var trophyFishWhitelist = map[string][]trophyFishCell{
	// level_001_lake
	"level_001_lake": {
		{key: "s.orfe", zh: "圆腹雅罗鱼"},
		{key: "c.roach", zh: "常见拟鲤"},
		{key: "ruffe", zh: "梅花鲈"},
		{key: "a.sleeper", zh: "葛氏鲈塘鳢"},
		{key: "c.carp", zh: "金鲫"},
		{key: "crucian", zh: "银鲫"},
		{key: "frog", zh: "青蛙"},
		{key: "perch", zh: "鲈鱼"},
	},
	// level_002_kirzah_river
	"level_002_kirzah_river": {
		{key: "gudgeon", zh: "常见鮈鱼"},
		{key: "c_bleak", zh: "欧鲌"},
		{key: "dreissena", zh: "河斑马纹贻贝"},
		{key: "crawfish", zh: "河螯虾"},
		{key: "freshwater_mussel", zh: "珠蚌"},
		{key: "e.chub", zh: "诸子鲦"},
		{key: "s.bream", zh: "银鲷鱼"},
		{key: "ruffe_n", zh: "长吻梅花鲈"},
		{key: "dace", zh: "雅罗鱼"},
		{key: "perch", zh: "鲈鱼"},
	},
	// level_003_viiveri_lake
	"level_003_viiveri_lake": {
		{key: "a_char", zh: "北极红点鲑"},
		{key: "s.orfe", zh: "圆腹雅罗鱼"},
		{key: "sevan_trout", zh: "塞凡湖鳟"},
		{key: "n.pike", zh: "常见狗鱼"},
		{key: "sig_kuor", zh: "廓里湖白鲑"},
		{key: "chartus_kuoriius", zh: "廓里湖红点鲑"},
		{key: "ruffe", zh: "梅花鲈"},
		{key: "vendace", zh: "欧白鲑"},
		{key: "bream", zh: "欧鳊"},
		{key: "burbot", zh: "江鳕"},
		{key: "gc_smelt", zh: "湖鳟鱼"},
		{key: "salvel_l", zh: "美洲红点鲑"},
		{key: "riv_rb_trout", zh: "红鳟"},
		{key: "perch", zh: "鲈鱼"},
	},
	// level_004_torfyanoe_ozero
	"level_004_torfyanoe_ozero": {
		{key: "tench", zh: "丁鱥"},
		{key: "c.roach", zh: "常见拟鲤"},
		{key: "n.pike", zh: "常见狗鱼"},
		{key: "carp", zh: "普通鲤鱼"},
		{key: "bream", zh: "欧鳊"},
		{key: "g_carp", zh: "草鱼"},
		{key: "a.sleeper", zh: "葛氏鲈塘鳢"},
		{key: "c.carp", zh: "金鲫"},
		{key: "crucian", zh: "银鲫"},
		{key: "s.bream", zh: "银鲷鱼"},
		{key: "frog", zh: "青蛙"},
		{key: "perch", zh: "鲈鱼"},
		{key: "eel", zh: "鳗鱼"},
		{key: "b_carp", zh: "黑鲩"},
	},
	// level_005_volhov
	"level_005_volhov": {
		{key: "b_bream", zh: "上口欧鳊"},
		{key: "s.orfe", zh: "圆腹雅罗鱼"},
		{key: "c_nase", zh: "大鼻软口鱼"},
		{key: "c.roach", zh: "常见拟鲤"},
		{key: "n.pike", zh: "常见狗鱼"},
		{key: "gudgeon", zh: "常见鮈鱼"},
		{key: "l_salmon", zh: "拉多加鲑"},
		{key: "v_bream", zh: "文鳊"},
		{key: "ruffe", zh: "梅花鲈"},
		{key: "e.pikeperch", zh: "梭鲈鱼"},
		{key: "vendace", zh: "欧白鲑"},
		{key: "bream", zh: "欧鳊"},
		{key: "burbot", zh: "江鳕"},
		{key: "wa_bream", zh: "白眼欧鳊"},
		{key: "asp", zh: "赤梢鱼"},
		{key: "s.bream", zh: "银鲷鱼"},
		{key: "dace", zh: "雅罗鱼"},
		{key: "perch", zh: "鲈鱼"},
	},
	// level_006_ladoga
	"level_006_ladoga": {
		{key: "stickleback_threesp", zh: "三刺鱼"},
		{key: "b_bream", zh: "上口欧鳊"},
		{key: "c.roach", zh: "常见拟鲤"},
		{key: "n.pike", zh: "常见狗鱼"},
		{key: "ripus", zh: "拉多加白鲑"},
		{key: "l_salmon", zh: "拉多加鲑"},
		{key: "v_bream", zh: "文鳊"},
		{key: "e.pikeperch", zh: "梭鲈鱼"},
		{key: "vendace", zh: "欧白鲑"},
		{key: "sig_vouksa", zh: "武克希白鲑"},
		{key: "sig", zh: "沃尔霍夫白鲑"},
		{key: "gc_smelt", zh: "湖鳟鱼"},
		{key: "sig_valaam", zh: "瓦拉姆白鲑"},
		{key: "palia_lud", zh: "粗壮红点鲑"},
		{key: "salvel_l", zh: "美洲红点鲑"},
		{key: "e_smelt", zh: "胡瓜鱼"},
		{key: "sig_ludoga", zh: "莱芒湖白鲑"},
		{key: "palia_kr", zh: "黑口红点鲑"},
	},
	// level_007_grass_lake
	"level_007_grass_lake": {
		{key: "tench", zh: "丁鱥"},
		{key: "c_barbel", zh: "常见鲃鱼"},
		{key: "n_carp", zh: "无鳞鲤"},
		{key: "carp", zh: "普通鲤鱼"},
		{key: "c_bleak", zh: "欧鲌"},
		{key: "g_carp", zh: "草鱼"},
		{key: "g_tench", zh: "金丁鱥"},
		{key: "c.carp", zh: "金鲫"},
		{key: "crucian", zh: "银鲫"},
		{key: "s.bream", zh: "银鲷鱼"},
		{key: "m_carp", zh: "镜鲤"},
		{key: "b_carp", zh: "黑鲩"},
	},
	// level_008_sura_river
	"level_008_sura_river": {
		{key: "common_carp", zh: "三花鲤鱼"},
		{key: "b_bream", zh: "上口欧鳊"},
		{key: "barsch", zh: "俄罗斯梭吻鲈"},
		{key: "r_sturgeon", zh: "俄罗斯鲟"},
		{key: "s.orfe", zh: "圆腹雅罗鱼"},
		{key: "c_nase", zh: "大鼻软口鱼"},
		{key: "sterlet", zh: "小体鲟"},
		{key: "n.pike", zh: "常见狗鱼"},
		{key: "e.pikeperch", zh: "梭鲈鱼"},
		{key: "w_catfish", zh: "欧洲六须鲇"},
		{key: "sabrefish", zh: "欧飘鱼"},
		{key: "rudd", zh: "红鳍鱼"},
		{key: "asp", zh: "赤梢鱼"},
		{key: "dace", zh: "雅罗鱼"},
		// 这一格原先按显示名认成了「鲑鱼」k_salmon，但苏拉河 37 种鱼里没有任何鲑科鱼，
		// 派不出来就永远上不了星。2026-11-11 核对后确认客户端那一格是「鲢鱼」s_carp
		// （本图确实能钓到，loc_fish 有 15 条记录、fish_ecology 也齐全），改回真 key。
		{key: "s_carp", zh: "鲢鱼"},
		{key: "bh_carp", zh: "鳙鱼"},
	},
	// level_009_ahtuba
	"level_009_ahtuba": {
		{key: "common_carp", zh: "三花鲤鱼"},
		{key: "b_bream", zh: "上口欧鳊"},
		{key: "e_bream", zh: "东方欧鳊"},
		{key: "r_sturgeon", zh: "俄罗斯鲟"},
		{key: "b_herring", zh: "勃氏西鲱"},
		{key: "s.orfe", zh: "圆腹雅罗鱼"},
		{key: "buffalo", zh: "大口水牛鱼"},
		{key: "c_nase", zh: "大鼻软口鱼"},
		{key: "sterlet", zh: "小体鲟"},
		{key: "n.pike", zh: "常见狗鱼"},
		{key: "stickleback_ss", zh: "平腹多刺鱼"},
		{key: "kutum", zh: "库图拟鲤"},
		{key: "shemaya", zh: "拟鲌"},
		{key: "ruffe", zh: "梅花鲈"},
		{key: "e.pikeperch", zh: "梭鲈鱼"},
		{key: "w_catfish", zh: "欧洲六须鲇"},
		{key: "sabrefish", zh: "欧飘鱼"},
		{key: "c_bleak", zh: "欧鲌"},
		{key: "burbot", zh: "江鳕"},
		{key: "p_sturgeon", zh: "波斯鲟"},
		{key: "vjun", zh: "泥鳅"},
		{key: "wa_bream", zh: "白眼欧鳊"},
		{key: "wfish", zh: "白鲑鱼"},
		{key: "sh_barbel", zh: "短头梭鲃"},
		{key: "rudd", zh: "红鳍鱼"},
		{key: "g_carp", zh: "草鱼"},
		{key: "osetr_ship", zh: "裸腹鲟"},
		{key: "asp", zh: "赤梢鱼"},
		{key: "vobla", zh: "里海拟鲤"},
		{key: "c_herring", zh: "里海西鲱"},
		{key: "k_salmon", zh: "里海鲑鱼"},
		{key: "st_sturgeon", zh: "闪光鲟"},
		{key: "dace", zh: "雅罗鱼"},
		{key: "s_carp", zh: "鲢鱼"},
		{key: "bh_carp", zh: "鳙鱼"},
		{key: "bl_buffalo", zh: "黑牛胭脂鱼"},
		{key: "k_herring", zh: "黑背西鲱"},
	},
	// level_010_belaya
	"level_010_belaya": {
		{key: "c.roach", zh: "常见拟鲤"},
		{key: "ruffe", zh: "梅花鲈"},
		{key: "c_bleak", zh: "欧鲌"},
		{key: "burbot", zh: "江鳕"},
		{key: "crawfish", zh: "河螯虾"},
		{key: "a_grayling", zh: "茴鱼"},
		{key: "e.chub", zh: "诸子鲦"},
		{key: "s.bream", zh: "银鲷鱼"},
		{key: "dace", zh: "雅罗鱼"},
		{key: "riv_brown_trout", zh: "鳟鱼"},
	},
	// level_011_ladoga_02
	"level_011_ladoga_02": {
		{key: "a_salmon", zh: "大西洋鲑鱼"},
		{key: "n.pike", zh: "常见狗鱼"},
		{key: "sig_o_l", zh: "拉多加湖湖泊白鲑"},
		{key: "ripus", zh: "拉多加白鲑"},
		{key: "l_salmon", zh: "拉多加鲑"},
		{key: "ruffe", zh: "梅花鲈"},
		{key: "e.pikeperch", zh: "梭鲈鱼"},
		{key: "vendace", zh: "欧白鲑"},
		{key: "sig_vouksa", zh: "武克希白鲑"},
		{key: "burbot", zh: "江鳕"},
		{key: "osetr_balt", zh: "波罗的海鲟鱼"},
		{key: "gc_smelt", zh: "湖鳟鱼"},
		{key: "sig_valaam", zh: "瓦拉姆白鲑"},
		{key: "palia_lud", zh: "粗壮红点鲑"},
		{key: "salvel_l", zh: "美洲红点鲑"},
		{key: "e_smelt", zh: "胡瓜鱼"},
		{key: "a_grayling", zh: "茴鱼"},
		{key: "sig_ludoga", zh: "莱芒湖白鲑"},
		{key: "perch", zh: "鲈鱼"},
		{key: "palia_kr", zh: "黑口红点鲑"},
		{key: "sig_black", zh: "黑白鲑"},
	},
	// level_012_north_sea
	"level_012_north_sea": {
		{key: "sebastes_n", zh: "北海平鲉"},
		{key: "menek", zh: "单鳍鳕"},
		{key: "pinagor", zh: "圆鳍鱼"},
		{key: "turbot", zh: "大菱鲆"},
		{key: "p_zubatka", zh: "大西洋狼鲷"},
		{key: "makrel", zh: "大西洋鲭"},
		{key: "atl_herring", zh: "大西洋鲱"},
		{key: "atl_cod", zh: "大西洋鳕"},
		{key: "d_zubatka", zh: "小齿狼鱼"},
		{key: "sebastes_m", zh: "尖吻平鲉"},
		{key: "halibut_w", zh: "庸鲽"},
		{key: "ambl_h", zh: "极北鳐"},
		{key: "merluza", zh: "欧洲无须鳕"},
		{key: "c_squid", zh: "欧洲鱿鱼"},
		{key: "sardina_p", zh: "沙丁鱼"},
		{key: "red_fish", zh: "海平鲉"},
		{key: "merlang", zh: "牙鳕"},
		{key: "katran", zh: "白斑角鲨"},
		{key: "m_scorpius", zh: "短角大杜父鱼"},
		{key: "s_saurus", zh: "竹刀鱼"},
		{key: "zoarces_v", zh: "绯鲷"},
		{key: "coalfish", zh: "绿青鳕"},
		{key: "m_zubatka", zh: "花狼鱼"},
		{key: "molva", zh: "蓝舒鲣"},
		{key: "poutassou", zh: "蓝鳕"},
		{key: "ambl_r", zh: "钝头鳐"},
		{key: "lh_kambala", zh: "长吻黄盖鲽"},
		{key: "hippoglos_p", zh: "长拟庸鲽"},
		{key: "pollachius", zh: "青鳕"},
		{key: "halibut_b", zh: "马舌鲽"},
		{key: "sea_kambala", zh: "鲆鲽"},
		{key: "piksha", zh: "黑线鳕"},
		{key: "centroloph_n", zh: "黑长鲷"},
	},
	// level_013_sev_don
	"level_013_sev_don": {
		{key: "tench", zh: "丁鱥"},
		{key: "common_carp", zh: "三花鲤鱼"},
		{key: "taran", zh: "亚速海拟鲤"},
		{key: "s.orfe", zh: "圆腹雅罗鱼"},
		{key: "c_nase", zh: "大鼻软口鱼"},
		{key: "n.pike", zh: "常见狗鱼"},
		{key: "e.pikeperch", zh: "梭鲈鱼"},
		{key: "tiulka", zh: "棱鲱"},
		{key: "sabrefish", zh: "欧飘鱼"},
		{key: "rudd", zh: "红鳍鱼"},
		{key: "g_carp", zh: "草鱼"},
		{key: "cha_herring", zh: "长体西鲱"},
		{key: "ruffe_n", zh: "长吻梅花鲈"},
		{key: "dace", zh: "雅罗鱼"},
		{key: "pumps_sunfish", zh: "驼背太阳鱼"},
		{key: "vyrezub", zh: "黑库图拟鲤"},
		{key: "shemaya_cha", zh: "黑海欧白鱼"},
	},
	// level_014_carp_lake
	"level_014_carp_lake": {
		{key: "tench", zh: "丁鱥"},
		{key: "m_carp_red", zh: "匈红鲤(镜鲤)"},
		{key: "carp_red", zh: "匈红鲤(鳞鲤)"},
		{key: "s.orfe", zh: "圆腹雅罗鱼"},
		{key: "l_carp", zh: "带形镜鲤"},
		{key: "l_carp_ghost", zh: "带形镜鲤(人面鲤)"},
		{key: "l_carp_albino", zh: "带形镜鲤(白化)"},
		{key: "n.pike", zh: "常见狗鱼"},
		{key: "c_barbel", zh: "常见鲃鱼"},
		{key: "n_carp", zh: "无鳞鲤"},
		{key: "n_carp_ghost", zh: "无鳞鲤 (人面鲤)"},
		{key: "n_carp_albino", zh: "无鳞鲤(白化)"},
		{key: "carp", zh: "普通鲤鱼"},
		{key: "r_carp", zh: "框形镜鲤"},
		{key: "r_carp_ghost", zh: "框形镜鲤(人面鲤)"},
		{key: "r_carp_albino", zh: "框形镜鲤(白化)"},
		{key: "bream", zh: "欧鳊"},
		{key: "g_carp", zh: "草鱼"},
		{key: "m_carp", zh: "镜鲤"},
		{key: "m_carp_ghost", zh: "镜鲤(人面鲤)"},
		{key: "m_carp_albino", zh: "镜鲤(白化)"},
		{key: "carp_ghost", zh: "鳞鲤(人面鲤)"},
		{key: "carp_albino", zh: "鳞鲤(白化)"},
	},
	// level_015_tunguska
	"level_015_tunguska": {
		{key: "os_grayling", zh: "东西伯利亚茴鱼"},
		{key: "zs_sturgeon", zh: "东西伯利亚鲟"},
		{key: "a_char", zh: "北极红点鲑"},
		{key: "zs_grayling", zh: "北极茴鱼"},
		{key: "taimen", zh: "哲罗鱼"},
		{key: "tugun", zh: "图冈白鲑"},
		{key: "chir", zh: "宽鼻白鲑"},
		{key: "syb_vendace", zh: "小白鲑"},
		{key: "syb_roach", zh: "湖拟鲤"},
		{key: "nelma", zh: "白北鲑"},
		{key: "valyok", zh: "真柱白鲑"},
		{key: "a_omul", zh: "秋白鲑"},
		{key: "muksun", zh: "穆松白鲑"},
		{key: "gorbusha", zh: "粉鲑"},
		{key: "lenok_o", zh: "细鳞鲑"},
		{key: "a_catfish", zh: "花鲶"},
		{key: "syb_sterlet", zh: "西伯利亚小体鲟"},
		{key: "b_omul", zh: "贝加尔白鲑"},
		{key: "syb_elets", zh: "贝加尔雅罗鱼"},
		{key: "sig_p", zh: "驼背白鲑"},
		{key: "peled", zh: "高白鲑"},
		{key: "dr_char", zh: "黄斑红点鲑"},
	},
	// level_016_yama
	"level_016_yama": {
		{key: "os_grayling", zh: "东西伯利亚茴鱼"},
		{key: "chukuchan", zh: "亚口鱼"},
		{key: "neiva", zh: "内维红点鲑"},
		{key: "a_char", zh: "北极红点鲑"},
		{key: "lenok_t", zh: "图们江细鳞鲑"},
		{key: "mikija", zh: "堪察加红鲑"},
		{key: "chavycha", zh: "大鳞麻哈鱼"},
		{key: "keta", zh: "大麻哈鱼"},
		{key: "trib_h", zh: "珠星三块鱼"},
		{key: "kunja", zh: "白斑红点鲑"},
		{key: "nerka", zh: "红大麻哈鱼"},
		{key: "malma", zh: "花羔红点斑鲑"},
		{key: "golets_lev", zh: "荣氏红点鲑"},
		{key: "chan_m", zh: "蒙古红鲃"},
		{key: "kijuch", zh: "银鲪鱼"},
		{key: "onkor_mg", zh: "马苏大麻哈鱼"},
		{key: "onkor_m", zh: "马苏鱼"},
	},
	// level_018_moskitnoe
	"level_018_moskitnoe": {
		{key: "s_freaks", zh: "C2超级怪物"},
		{key: "din_l_carp", zh: "丁克斯带形鲤"},
		{key: "din_m_carp", zh: "丁克斯镜鲤"},
		{key: "tench", zh: "丁鱥"},
		{key: "s.orfe", zh: "圆腹雅罗鱼"},
		{key: "c_barbel", zh: "常见鲃鱼"},
		{key: "kw_tench", zh: "库尔斯多夫丁鱥"},
		{key: "n_carp", zh: "无鳞鲤"},
		{key: "carp", zh: "普通鲤鱼"},
		{key: "o_ogon", zh: "橙黄金锦鲤"},
		{key: "bream", zh: "欧鳊"},
		{key: "c.carp", zh: "金鲫"},
		{key: "crucian", zh: "银鲫"},
		{key: "m_carp", zh: "镜鲤"},
		{key: "karasekarp", zh: "鲢鲤"},
	},
	// level_019_american_pond（埃尔克湖）17 格：本图 19 种鱼里只有斑点雀鳝 sgar 与
	// 鳄雀鳝 agar 不在面板上，其余全收（2026-11-11 面板截图核对）。
	"level_019_american_pond": {
		{key: "lm_b_bass", zh: "大口黑鲈"},
		{key: "warm", zh: "太阳鱼"},
		{key: "resfish", zh: "小冠太阳鱼"},
		{key: "sm_bass", zh: "小口黑鲈"},
		{key: "ch_catfish", zh: "斑点叉尾鮰"},
		{key: "sp_bass", zh: "斑点黑鲈"},
		{key: "hs_bass", zh: "杂交条纹狼鲈"},
		{key: "s_bass", zh: "条纹狼鲈"},
		{key: "fwdrum", zh: "淡水石首鱼"},
		{key: "walleye", zh: "玻璃梭鲈"},
		{key: "white_crappie", zh: "白刺盖太阳鱼"},
		{key: "bp_bass", zh: "眼点丽鱼"},
		{key: "g_shad", zh: "美洲真鰶"},
		{key: "b_gill", zh: "蓝鳃太阳鱼"},
		{key: "wh_bass", zh: "金眼狼鲈"},
		{key: "fhcatfish", zh: "铲鮰"},
		{key: "black_crappie", zh: "黑斑刺盖太阳鱼"},
	},
}

// trophyFishWhitelistMissing 记录「面板上有、但我们认不出 key」的格子数：截图里能看到
// 卡片，可是服务端 252 个鱼种里没有对得上号的（属于客户端独有、本服没有的鱼）。这些
// 格子永远不可能上星，于是把分母补上、但分子永远不加——奖杯不会误发，等以后补齐鱼种
// 再把这一项删掉。
var trophyFishWhitelistMissing = map[string]int{
	"level_015_tunguska":  1, // 面板上的「鲟鱼」：除东西伯利亚鲟/西伯利亚小体鲟之外的第三种鲟
	"level_012_north_sea": 1, // 面板上的「鲣鱼」：5.004kg/62cm，对不上任何已有 key
}

// trophyFishWhitelistUnspawnable 记录「面板上有、本服这张图却派不出」的鱼种 key。
//
// 目前为空：唯一的一格（苏拉河「鲑鱼」）在 2026-11-11 核对后确认是「鲢鱼」s_carp，
// 本图能正常派鱼，已改回真 key，不再需要扣分母。机制留着给将来新发现的数据缺口用。
//
// 与 trophyFishWhitelistMissing 的区别：missing 是「认不出是哪种鱼」（分母 +1、
// 分子永远不加，宁可晚发也不误发）；unspawnable 是「认得出，但本服钓不到」（分母
// 一并扣掉，不阻塞）。两者都是数据缺口，补齐鱼种或确认真实 key 后删掉对应条目即可。
var trophyFishWhitelistUnspawnable = map[string][]string{}

// trophyFishCell 是面板上一个格子：key 是 catch_log.fish_key，zh 是客户端显示名
// （只用于注释/排障，判定一律用 key）。
type trophyFishCell struct {
	key string
	zh  string
}

// trophyFishWhitelistSet 返回该水域的面板收录清单（集合形式）。没有清单的水域返回
// nil，调用方需要退回旧口径（loc_fish 全集）。
func trophyFishWhitelistSet(placeID string) map[string]struct{} {
	cells := trophyFishWhitelist[placeID]
	if len(cells) == 0 {
		return nil
	}
	skip := trophyFishWhitelistUnspawnableSet(placeID)
	set := make(map[string]struct{}, len(cells))
	for _, c := range cells {
		if c.key == "" {
			continue
		}
		if _, bad := skip[c.key]; bad {
			continue
		}
		set[c.key] = struct{}{}
	}
	return set
}

// trophyFishWhitelistUnspawnableSet 返回该水域「本服派不出」的 key 集合（可能为 nil）。
func trophyFishWhitelistUnspawnableSet(placeID string) map[string]struct{} {
	keys := trophyFishWhitelistUnspawnable[placeID]
	if len(keys) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		set[k] = struct{}{}
	}
	return set
}

// trophyFishWhitelistTotal 返回该水域面板的格子总数（含认不出的那些）。0 表示没有清单。
func trophyFishWhitelistTotal(placeID string) int {
	cells := trophyFishWhitelist[placeID]
	if len(cells) == 0 {
		return 0
	}
	skip := trophyFishWhitelistUnspawnableSet(placeID)
	reachable := 0
	for _, c := range cells {
		if c.key == "" {
			continue
		}
		if _, bad := skip[c.key]; bad {
			continue
		}
		reachable++
	}
	return reachable + trophyFishWhitelistMissing[placeID]
}
