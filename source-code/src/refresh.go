package src

func HandleRefresh() {
	PrintInfo("Force-refreshing repository list...")
	if LoadRepo(true) {
		PrintSuccess("Repository refreshed")
	}
}
