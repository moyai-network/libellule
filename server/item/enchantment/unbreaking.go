package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/armour"
	"github.com/df-mc/dragonfly/server/item/tool"
	"github.com/df-mc/dragonfly/server/world"
	"math/rand"
)

// Efficiency is an enchantment that increases mining speed.
type Unbreaking struct{ enchantment }

// Reduce returns the amount of damage that should be reduced with unbreaking.
func (e Unbreaking) Reduce(it world.Item, level, amount int) int {
	after := amount

	_, ok := it.(armour.Armour)
	for i := 0; i < amount; i++ {
		if (!ok || rand.Float64() >= 0.6) && rand.Intn(level+1) > 0 {
			after--
		}
	}

	return after
}

// Name ...
func (e Unbreaking) Name() string {
	return "Unbreaking"
}

// MaxLevel ...
func (e Unbreaking) MaxLevel() int {
	return 5
}

// WithLevel ...
func (e Unbreaking) WithLevel(level int) item.Enchantment {
	return Unbreaking{e.withLevel(level, e)}
}

// CompatibleWith ...
func (e Unbreaking) CompatibleWith(s item.Stack) bool {
	t, ok := s.Item().(tool.Tool)
	return ok && t.ToolType() == tool.TypePickaxe
}