package skills

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/igorsilveira/pincer/pkg/llm"
)

type Skill struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Version     string               `json:"version"`
	Author      string               `json:"author"`
	Tools       []llm.ToolDefinition `json:"tools"`
	Prompt      string               `json:"prompt"`
	Policy      Policy               `json:"policy"`
	Signature   string               `json:"signature"`
	Verified    bool                 `json:"-"`
}

type Policy struct {
	Network      bool     `json:"network"`
	Filesystem   bool     `json:"filesystem"`
	Shell        bool     `json:"shell"`
	AllowedPaths []string `json:"allowed_paths,omitempty"`
	MaxTimeout   string   `json:"max_timeout,omitempty"`
}

func LoadFromFile(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("skills: reading %s: %w", path, err)
	}

	var sk Skill
	if err := json.Unmarshal(data, &sk); err != nil {
		return nil, fmt.Errorf("skills: parsing %s: %w", path, err)
	}

	if sk.Name == "" {
		return nil, fmt.Errorf("skills: %s: name is required", path)
	}

	return &sk, nil
}

func (sk *Skill) Verify(publicKey ed25519.PublicKey) bool {
	if sk.Signature == "" {
		return false
	}

	sig, err := hex.DecodeString(sk.Signature)
	if err != nil {
		return false
	}

	digest := sk.contentDigest()
	sk.Verified = ed25519.Verify(publicKey, digest, sig)
	return sk.Verified
}

func (sk *Skill) Sign(privateKey ed25519.PrivateKey) {
	digest := sk.contentDigest()
	sig := ed25519.Sign(privateKey, digest)
	sk.Signature = hex.EncodeToString(sig)
	sk.Verified = true
}

func (sk *Skill) contentDigest() []byte {
	h := sha256.New()
	h.Write([]byte(sk.Name))
	h.Write([]byte(sk.Version))
	h.Write([]byte(sk.Author))
	h.Write([]byte(sk.Prompt))

	toolBytes, _ := json.Marshal(sk.Tools)
	h.Write(toolBytes)

	policyBytes, _ := json.Marshal(sk.Policy)
	h.Write(policyBytes)

	return h.Sum(nil)
}

func LoadDir(dir string) ([]*Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("skills: reading dir %s: %w", dir, err)
	}

	var skills []*Skill
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		sk, err := LoadFromFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		skills = append(skills, sk)
	}
	return skills, nil
}
