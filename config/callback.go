package config

type ConfigCallback[T any] struct {
	callbacks []func(T)
}

func (cc *ConfigCallback[T]) AddCallback(f func(T)) {
	cc.callbacks = append(cc.callbacks, f)
}

func (cc *ConfigCallback[T]) Call(config T) {
	for _, gc := range cc.callbacks {
		gc(config)
	}
}
