package mcver

import "strconv"

type weeklyCycle struct {
	first   int
	last    int
	release string
}

// Inclusive year*100+week bounds. 1.16.5 shipped in the middle of the 1.17 cycle,
// so 20w45a-20w51a belong to 1.17, not to the release that followed them by date.
var weeklyCycles = []weeklyCycle{
	{1843, 1914, "1.14"},
	{1934, 1946, "1.15"},
	{2006, 2022, "1.16"},
	{2027, 2030, "1.16.2"},
	{2045, 2120, "1.17"},
	{2137, 2144, "1.18"},
	{2203, 2207, "1.18.2"},
	{2211, 2219, "1.19"},
	{2224, 2224, "1.19.1"},
	{2242, 2246, "1.19.3"},
	{2303, 2307, "1.19.4"},
	{2312, 2318, "1.20"},
	{2331, 2335, "1.20.2"},
	{2340, 2346, "1.20.3"},
	{2351, 2414, "1.20.5"},
	{2418, 2421, "1.21"},
	{2433, 2440, "1.21.2"},
	{2444, 2446, "1.21.4"},
	{2502, 2510, "1.21.5"},
	{2515, 2521, "1.21.6"},
	{2531, 2537, "1.21.9"},
	{2541, 2546, "1.21.11"},
}

func weeklyRelease(week int) (string, bool) {
	for _, c := range weeklyCycles {
		if week >= c.first && week <= c.last {
			return c.release, true
		}
	}
	return "", false
}

// WeeklyRelease is the release a weekly snapshot such as 24w33a led to.
func WeeklyRelease(id string) (string, bool) {
	m := weeklyRe.FindStringSubmatch(id)
	if m == nil {
		return "", false
	}
	year, _ := strconv.Atoi(m[1])
	week, _ := strconv.Atoi(m[2])
	return weeklyRelease(year*100 + week)
}
