package config

import "testing"

// HeredocWords follows BuildKit: a heredoc is an unquoted word `[digits]<<[-]WORD`, the delimiter
// being WORD with its quotes and backslashes removed.
func TestHeredocWords(t *testing.T) {
	cases := map[string][]string{
		"cat <<EOF > /x":        {"EOF"},
		"cat <<-EOF":            {"EOF"},
		`cat <<\EOF`:            {"EOF"},
		`cat <<-\EOF`:           {"EOF"},
		`cat <<''EOF`:           {"EOF"},
		`cat <<"\EOF"`:          {"EOF"},
		"cat <<END-X":           {"END-X"},
		"cat 3<<EOF":            {"EOF"},
		"cat <<1X":              {"1X"},
		"cat <<.X":              {".X"},
		"cat <<A <<B":           {"A", "B"},
		`echo "<<EOF"`:          nil,
		`echo '<<EOF'`:          nil,
		"echo $((1<<FOO))":      nil,
		"a<<X":                  nil,
		`cat <<< "here string"`: nil,
		"tr a b <<<word":        nil,
	}
	for line, want := range cases {
		var got []string
		for _, h := range HeredocWords(line) {
			got = append(got, h.Delimiter)
		}
		if len(got) != len(want) {
			t.Errorf("%q: delimiters %v, want %v", line, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%q: delimiters %v, want %v", line, got, want)
			}
		}
	}
	if h := HeredocWords("cat <<-EOF"); len(h) != 1 || !h[0].StripTabs {
		t.Errorf("<<- must strip tabs: %v", h)
	}
}

// BuildKit leaves `<< EOF` to the shell, but the shell still waits for a body there.
func TestHasShellHeredoc(t *testing.T) {
	cases := map[string]bool{
		"cat <<EOF":          true,
		"cat << 'EOF' > /x":  true,
		"cat <<- EOF":        true,
		"echo <<":            false,
		`echo "<< EOF"`:      false,
		"echo $((1 << 4))":   false,
		"cat <<< 'a string'": false,
	}
	for line, want := range cases {
		if got := HasShellHeredoc(line); got != want {
			t.Errorf("HasShellHeredoc(%q) = %v, want %v", line, got, want)
		}
	}
}
