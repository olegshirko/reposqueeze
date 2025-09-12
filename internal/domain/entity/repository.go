package entity

// Repository represents a Git repository.
type Repository struct {
	Path string
}

// Project represents a GitLab project.
type Project struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}