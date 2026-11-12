package bpmn

// membershipIDPointer 把 0 归一为 NULL，供 ent 的可空 membership 列使用。
func membershipIDPointer(id int) *int {
	if id <= 0 {
		return nil
	}
	return &id
}
