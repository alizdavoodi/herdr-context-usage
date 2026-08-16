package report

func ShouldWrite(value int64, existing map[string]string) bool {
	if value != 0 {
		return true
	}
	return existing["ctx_raw"] == ""
}
