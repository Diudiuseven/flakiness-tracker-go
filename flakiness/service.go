package flakiness

// Service 是面向调用方的门面，组合存储、度量、策略、基线与放行判定。
type Service struct {
	store *Store
}

// NewService 创建服务实例。
func NewService() *Service {
	return &Service{store: NewStore()}
}

// Store 返回底层存储。
func (s *Service) Store() *Store { return s.store }
