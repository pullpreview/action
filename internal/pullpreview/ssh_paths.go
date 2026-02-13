package pullpreview

func HomeDirForUser(username string) string {
	if username == "root" {
		return "/root"
	}
	return "/home/" + username
}

