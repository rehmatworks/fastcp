package agent

import "strings"

// parseMySQLVersion turns `mysql --version` output into "MySQL 8.0.39" or
// "MariaDB 10.11.6". MySQL prints "Ver 8.0.39-0ubuntu...", MariaDB prints
// "Distrib 10.11.6-MariaDB," or "from 11.4.2-MariaDB,".
func parseMySQLVersion(out string) string {
	f := strings.Fields(out)
	for _, marker := range []string{"Distrib", "from", "Ver"} {
		for i, s := range f {
			if s != marker || i+1 >= len(f) {
				continue
			}
			v := strings.TrimSuffix(f[i+1], ",")
			product := "MySQL"
			if strings.Contains(strings.ToLower(v), "mariadb") {
				product = "MariaDB"
			}
			if j := strings.IndexByte(v, '-'); j > 0 {
				v = v[:j]
			}
			return product + " " + v
		}
	}
	return ""
}
