package review

// Comment represents a reviewer's inline comment.
type Comment struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Review holds all comments and metadata for a code review session.
type Review struct {
	Comments       []Comment `json:"comments"`
	GeneralComment string    `json:"general_comment,omitempty"`
	CommitHash     string    `json:"commit_hash,omitempty"`
	CommitSubject  string    `json:"commit_subject,omitempty"`
}

func New() *Review {
	return &Review{}
}

func (r *Review) AddComment(file string, line int, text string) {
	r.Comments = append(r.Comments, Comment{
		File: file,
		Line: line,
		Text: text,
	})
}

func (r *Review) DeleteComment(file string, line int) {
	var kept []Comment
	for _, c := range r.Comments {
		if c.File == file && c.Line == line {
			continue
		}
		kept = append(kept, c)
	}
	r.Comments = kept
}

func (r *Review) CommentsForFile(file string) []Comment {
	var result []Comment
	for _, c := range r.Comments {
		if c.File == file {
			result = append(result, c)
		}
	}
	return result
}
