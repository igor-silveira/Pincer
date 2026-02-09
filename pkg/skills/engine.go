package skills

import (
	"crypto/ed25519"
	"fmt"
	"sync"
)

type Engine struct {
	mu            sync.RWMutex
	skills        map[string]*Skill
	trustedKeys   []ed25519.PublicKey
	allowUnsigned bool
	skillDir      string
}

type EngineConfig struct {
	SkillDir      string
	TrustedKeys   []ed25519.PublicKey
	AllowUnsigned bool
}

func NewEngine(cfg EngineConfig) *Engine {
	return &Engine{
		skills:        make(map[string]*Skill),
		trustedKeys:   cfg.TrustedKeys,
		allowUnsigned: cfg.AllowUnsigned,
		skillDir:      cfg.SkillDir,
	}
}

func (e *Engine) LoadAll() ([]ScanResult, error) {
	if e.skillDir == "" {
		return nil, nil
	}

	skills, err := LoadDir(e.skillDir)
	if err != nil {
		return nil, fmt.Errorf("skills: loading directory: %w", err)
	}

	var results []ScanResult
	for _, sk := range skills {
		result, err := e.Install(sk)
		if err != nil {
			return results, fmt.Errorf("skills: installing %q: %w", sk.Name, err)
		}
		results = append(results, result)
	}

	return results, nil
}

func (e *Engine) Install(sk *Skill) (ScanResult, error) {

	if !e.verifySignature(sk) && !e.allowUnsigned {
		return ScanResult{}, fmt.Errorf("skill %q has no valid signature (use --allow-unsigned to override)", sk.Name)
	}

	result := Scan(sk)
	if !result.Safe {
		return result, fmt.Errorf("skill %q failed static analysis with %d findings", sk.Name, len(result.Findings))
	}

	e.mu.Lock()
	e.skills[sk.Name] = sk
	e.mu.Unlock()

	return result, nil
}

func (e *Engine) Get(name string) (*Skill, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	sk, ok := e.skills[name]
	return sk, ok
}

func (e *Engine) List() []*Skill {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*Skill, 0, len(e.skills))
	for _, sk := range e.skills {
		out = append(out, sk)
	}
	return out
}

func (e *Engine) Uninstall(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.skills[name]
	delete(e.skills, name)
	return ok
}

func (e *Engine) verifySignature(sk *Skill) bool {
	for _, key := range e.trustedKeys {
		if sk.Verify(key) {
			return true
		}
	}
	return false
}
