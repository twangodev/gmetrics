package people

type Person struct {
	Login string
	// Empty when the avatar fetch was skipped; Render draws a placeholder.
	AvatarB64 string
}

type Section struct {
	Type string
	// GitHub's upstream total, which may exceed len(People) under Config.Limit.
	Total  int
	People []Person
}

type Data struct {
	Sections []Section
	Size     int
}
