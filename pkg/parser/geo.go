package parser

import (
	"strings"
)

type GeoInfo struct {
	Country string
	Flag    string
}

// ExtractGeo attempts to deduce country and flag emoji from node name
func ExtractGeo(name string) GeoInfo {
	lower := strings.ToLower(name)

	rules := []struct {
		keywords []string
		country  string
		flag     string
	}{
		{[]string{"hk", "hongkong", "hong kong", "香港"}, "Hong Kong", "🇭🇰"},
		{[]string{"jp", "japan", "日本", "东京", "tokyo", "osaka", "大阪"}, "Japan", "🇯🇵"},
		{[]string{"us", "united states", "usa", "america", "美国", "洛杉矶", "圣何塞", "硅谷", "纽约", "凤凰城"}, "United States", "🇺🇸"},
		{[]string{"sg", "singapore", "新加坡", "狮城"}, "Singapore", "🇸🇬"},
		{[]string{"tw", "taiwan", "台湾", "台北"}, "Taiwan", "🇹🇼"},
		{[]string{"kr", "korea", "韩国", "首尔", "seoul"}, "South Korea", "🇰🇷"},
		{[]string{"gb", "uk", "united kingdom", "英国", "伦敦", "london"}, "United Kingdom", "🇬🇧"},
		{[]string{"de", "germany", "德国", "法兰克福", "frankfurt"}, "Germany", "🇩🇪"},
		{[]string{"fr", "france", "法国", "巴黎", "paris"}, "France", "🇫🇷"},
		{[]string{"ca", "canada", "加拿大", "温哥华", "多伦多"}, "Canada", "🇨🇦"},
		{[]string{"au", "australia", "澳大利亚", "澳洲", "悉尼", "sydney", "melbourne"}, "Australia", "🇦🇺"},
		{[]string{"ru", "russia", "俄罗斯", "莫斯科", "moscow"}, "Russia", "🇷🇺"},
		{[]string{"nl", "netherlands", "荷兰", "阿姆斯特丹"}, "Netherlands", "🇳🇱"},
		{[]string{"in", "india", "印度", "孟买", "mumbai"}, "India", "🇮🇳"},
		{[]string{"th", "thailand", "泰国", "曼谷", "bangkok"}, "Thailand", "🇹🇭"},
		{[]string{"vn", "vietnam", "越南", "河内", "胡志明"}, "Vietnam", "🇻🇳"},
		{[]string{"my", "malaysia", "马来西亚", "吉隆坡"}, "Malaysia", "🇲🇾"},
		{[]string{"ph", "philippines", "菲律宾", "马尼拉"}, "Philippines", "🇵🇭"},
		{[]string{"br", "brazil", "巴西", "圣保罗"}, "Brazil", "🇧🇷"},
		{[]string{"ae", "uae", "dubai", "阿联酋", "迪拜"}, "United Arab Emirates", "🇦🇪"},
		{[]string{"tr", "turkey", "土耳其", "伊斯坦布尔"}, "Turkey", "🇹🇷"},
		{[]string{"ch", "switzerland", "瑞士", "苏黎世"}, "Switzerland", "🇨🇭"},
		{[]string{"se", "sweden", "瑞典", "斯德哥尔摩"}, "Sweden", "🇸🇪"},
		{[]string{"es", "spain", "西班牙", "马德里"}, "Spain", "🇪🇸"},
		{[]string{"it", "italy", "意大利", "罗马", "米兰"}, "Italy", "🇮🇹"},
	}

	for _, rule := range rules {
		for _, kw := range rule.keywords {
			if strings.Contains(lower, kw) {
				return GeoInfo{Country: rule.country, Flag: rule.flag}
			}
		}
	}

	return GeoInfo{Country: "Global", Flag: "🌐"}
}
