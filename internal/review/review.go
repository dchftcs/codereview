package review

// Comment represents a reviewer's inline comment.
type Comment struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	EndLine int    `json:"end_line,omitempty"` // 0 means single-line comment
	Text    string `json:"text"`
}

// Review holds all comments and metadata for a code review session.
type Review struct {
	Comments        []Comment `json:"comments"`
	GeneralComments []string  `json:"general_comments,omitempty"`
	CommitHash      string    `json:"commit_hash,omitempty"`
	CommitSubject   string    `json:"commit_subject,omitempty"`
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

func (r *Review) AddRangeComment(file string, startLine, endLine int, text string) {
	r.Comments = append(r.Comments, Comment{
		File:    file,
		Line:    startLine,
		EndLine: endLine,
		Text:    text,
	})
}

func (r *Review) AddGeneralComment(text string) {
	if text == "" {
		r.GeneralComments = nil
		return
	}
	r.GeneralComments = []string{text}
}

func (r *Review) DeleteGeneralComment(index int) {
	if index != 0 || len(r.GeneralComments) == 0 {
		return
	}
	r.GeneralComments = nil
}

func (r *Review) EditGeneralComment(index int, text string) {
	if index != 0 || len(r.GeneralComments) == 0 {
		return
	}
	if text == "" {
		r.GeneralComments = nil
		return
	}
	r.GeneralComments = []string{text}
}

func (r *Review) GeneralComment() string {
	if len(r.GeneralComments) == 0 {
		return ""
	}
	return r.GeneralComments[0]
}

func (r *Review) DeleteComment(file string, line int) {
	r.DeleteCommentRange(file, line, 0)
}

func (r *Review) DeleteCommentRange(file string, line, endLine int) {
	var kept []Comment
	for _, c := range r.Comments {
		if c.File == file && c.Line == line && c.EndLine == endLine {
			continue
		}
		kept = append(kept, c)
	}
	r.Comments = kept
}

func (r *Review) FindComment(file string, line int) *Comment {
	return r.FindCommentRange(file, line, 0)
}

func (r *Review) FindCommentRange(file string, line, endLine int) *Comment {
	for i := range r.Comments {
		if r.Comments[i].File == file && r.Comments[i].Line == line && r.Comments[i].EndLine == endLine {
			return &r.Comments[i]
		}
	}
	return nil
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
