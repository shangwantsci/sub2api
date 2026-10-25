package admin

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type inMemSettingRepo struct{ data map[string]string }

func (r *inMemSettingRepo) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}
func (r *inMemSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if v, ok := r.data[key]; ok {
		return v, nil
	}
	return "", service.ErrSettingNotFound
}
func (r *inMemSettingRepo) Set(_ context.Context, key, value string) error {
	r.data[key] = value
	return nil
}
func (r *inMemSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := r.data[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}
func (r *inMemSettingRepo) SetMultiple(_ context.Context, settings map[string]string) error {
	for k, v := range settings {
		r.data[k] = v
	}
	return nil
}
func (r *inMemSettingRepo) GetAll(context.Context) (map[string]string, error) { return r.data, nil }
func (r *inMemSettingRepo) Delete(_ context.Context, key string) error {
	delete(r.data, key)
	return nil
}
