package block

import (
	"fmt"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Hopper is a low-capacity storage block that can be used to collect item entities directly above it, as well as to
// transfer items into and out of other containers.
type Hopper struct {
	transparent
	sourceWaterDisplacer

	// Facing is the direction the hopper is facing.
	Facing cube.Face
	// Powered is whether the hopper is powered or not.
	Powered bool
	// CustomName is the custom name of the hopper. This name is displayed when the hopper is opened, and may include
	// colour codes.
	CustomName string

	// LastTick is the last world tick that the hopper was ticked.
	LastTick int64
	// TransferCooldown is the duration until the hopper can transfer items again.
	TransferCooldown int64
	// CollectCooldown is the duration until the hopper can collect items again.
	CollectCooldown int64

	inventory *inventory.Inventory
	viewerMu  *sync.RWMutex
	viewers   map[ContainerViewer]struct{}
}

// NewHopper creates a new initialised hopper. The inventory is properly initialised.
func NewHopper() Hopper {
	m := new(sync.RWMutex)
	v := make(map[ContainerViewer]struct{}, 1)
	return Hopper{
		inventory: inventory.New(5, func(slot int, _, item item.Stack) {
			m.RLock()
			defer m.RUnlock()
			for viewer := range v {
				viewer.ViewSlotChange(slot, item)
			}
		}),
		viewerMu: m,
		viewers:  v,
	}
}

// Model ...
func (Hopper) Model() world.BlockModel {
	return model.Hopper{}
}

// SideClosed ...
func (Hopper) SideClosed(cube.Pos, cube.Pos, *world.World) bool {
	return false
}

// BreakInfo ...
func (h Hopper) BreakInfo() BreakInfo {
	return newBreakInfo(3, pickaxeHarvestable, pickaxeEffective, oneOf(h)).withBlastResistance(24)
}

// Inventory returns the inventory of the hopper.
func (h Hopper) Inventory(w *world.World, pos cube.Pos) *inventory.Inventory {
	return h.inventory
}

// WithName returns the hopper after applying a specific name to the block.
func (h Hopper) WithName(a ...any) world.Item {
	h.CustomName = strings.TrimSuffix(fmt.Sprintln(a...), "\n")
	return h
}

// AddViewer adds a viewer to the hopper, so that it is updated whenever the inventory of the hopper is changed.
func (h Hopper) AddViewer(v ContainerViewer, _ *world.World, _ cube.Pos) {
	h.viewerMu.Lock()
	defer h.viewerMu.Unlock()
	h.viewers[v] = struct{}{}
}

// RemoveViewer removes a viewer from the hopper, so that slot updates in the inventory are no longer sent to it.
func (h Hopper) RemoveViewer(v ContainerViewer, _ *world.World, _ cube.Pos) {
	h.viewerMu.Lock()
	defer h.viewerMu.Unlock()
	delete(h.viewers, v)
}

// Activate ...
func (Hopper) Activate(pos cube.Pos, _ cube.Face, _ *world.World, u item.User, _ *item.UseContext) bool {
	if opener, ok := u.(ContainerOpener); ok {
		opener.OpenBlockContainer(pos)
		return true
	}
	return false
}

// UseOnBlock ...
func (h Hopper) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, w *world.World, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(w, pos, face, h)
	if !used {
		return false
	}

	//noinspection GoAssignmentToReceiver
	h = NewHopper()
	h.Facing = cube.FaceDown
	if h.Facing != face {
		h.Facing = face.Opposite()
	}

	place(w, pos, h, user, ctx)
	return placed(ctx)
}

// Tick ...
func (h Hopper) Tick(currentTick int64, pos cube.Pos, w *world.World) {
	h.TransferCooldown--
	h.CollectCooldown--
	h.LastTick = currentTick

	if h.TransferCooldown >= 0 || h.CollectCooldown >= 0 {
		w.SetBlock(pos, h, nil)
		return
	}

	h.TransferCooldown = 0
	h.CollectCooldown = 0
	if h.Powered {
		w.SetBlock(pos, h, nil)
		return
	}

	inserted := h.insertItem(pos, w)
	extracted := h.extractItem(pos, w)
	if inserted || extracted {
		h.TransferCooldown = 8
		w.SetBlock(pos, h, nil)
	}
}

// HopperInsertable represents a block that can have its contents inserted into by a hopper.
type HopperInsertable interface {
	Container

	// InsertItem attempts to insert a single item into the container. If the insertion was successful, the item is
	// returned. If the insertion was unsuccessful, the item stack returned will be empty. InsertItem by itself does
	// should not add the item to the container, but instead return the item that would be added.
	InsertItem(item.Stack, cube.Face) (bool, int)
}

// insertItem inserts an item into a container from the hopper.
func (h Hopper) insertItem(pos cube.Pos, w *world.World) bool {
	dest, ok := w.Block(pos.Side(h.Facing)).(Container)
	if !ok || dest.Inventory(w, pos) == nil {
		return false
	}

	for sourceSlot, sourceStack := range h.inventory.Slots() {
		if sourceStack.Empty() {
			continue
		}

		if e, ok := dest.(HopperInsertable); !ok {
			_, err := dest.Inventory(w, pos).AddItem(sourceStack.Grow(-sourceStack.Count() + 1))
			if err != nil {
				// The destination is full.
				continue
			}
		} else {
			stack := sourceStack.Grow(-sourceStack.Count() + 1)
			allowed, targetSlot := e.InsertItem(stack, h.Facing)
			it, _ := e.Inventory(w, pos).Item(targetSlot)
			if !allowed || !sourceStack.Comparable(it) {
				// The items are not the same.
				continue
			}
			if !it.Empty() {
				stack = it.Grow(1)
			}

			_ = dest.Inventory(w, pos).SetItem(targetSlot, stack)
		}

		_ = h.inventory.SetItem(sourceSlot, sourceStack.Grow(-1))
		return true
	}
	return false
}

// HopperExtractable represents a block that can have its contents extracted by a hopper.
type HopperExtractable interface {
	Container

	// ExtractItem attempts to extract a single item from the container. If the extraction was successful, the item is
	// returned. If the extraction was unsuccessful, the item stack returned will be empty. ExtractItem by itself does
	// should not remove the item from the container, but instead return the item that would be removed.
	ExtractItem() (item.Stack, int)
}

// extractItem extracts an item from a container into the hopper.
func (h Hopper) extractItem(pos cube.Pos, w *world.World) bool {
	origin, ok := w.Block(pos.Side(cube.FaceUp)).(Container)
	if !ok || origin.Inventory(w, pos) == nil {
		return false
	}

	var (
		targetSlot  int
		targetStack item.Stack
	)
	if e, ok := origin.(HopperExtractable); !ok {
		for slot, stack := range origin.Inventory(w, pos).Slots() {
			if stack.Empty() {
				continue
			}
			targetStack, targetSlot = stack, slot
			break
		}
	} else {
		targetStack, targetSlot = e.ExtractItem()
	}
	if targetStack.Empty() {
		// We don't have any items to extract.
		return false
	}

	_, err := h.inventory.AddItem(targetStack.Grow(-targetStack.Count() + 1))
	if err != nil {
		// The hopper is full.
		return false
	}
	_ = origin.Inventory(w, pos).SetItem(targetSlot, targetStack.Grow(-1))
	return true
}

// EncodeItem ...
func (Hopper) EncodeItem() (name string, meta int16) {
	return "minecraft:hopper", 0
}

// EncodeBlock ...
func (h Hopper) EncodeBlock() (string, map[string]any) {
	return "minecraft:hopper", map[string]any{
		"facing_direction": int32(h.Facing),
		"toggle_bit":       h.Powered,
	}
}

// EncodeNBT ...
func (h Hopper) EncodeNBT() map[string]any {
	if h.inventory == nil {
		facing, powered, customName := h.Facing, h.Powered, h.CustomName
		//noinspection GoAssignmentToReceiver
		h = NewHopper()
		h.Facing, h.Powered, h.CustomName = facing, powered, customName
	}
	m := map[string]any{
		"Items":            nbtconv.InvToNBT(h.inventory),
		"TransferCooldown": int32(h.TransferCooldown),
		"id":               "Hopper",
	}
	if h.CustomName != "" {
		m["CustomName"] = h.CustomName
	}
	return m
}

// DecodeNBT ...
func (h Hopper) DecodeNBT(data map[string]any) any {
	facing, powered := h.Facing, h.Powered
	//noinspection GoAssignmentToReceiver
	h = NewHopper()
	h.Facing = facing
	h.Powered = powered
	h.CustomName = nbtconv.String(data, "CustomName")
	h.TransferCooldown = int64(nbtconv.Int32(data, "TransferCooldown"))
	nbtconv.InvFromNBT(h.inventory, nbtconv.Slice(data, "Items"))
	return h
}

// allHoppers ...
func allHoppers() (hoppers []world.Block) {
	for _, f := range cube.Faces() {
		hoppers = append(hoppers, Hopper{Facing: f})
		hoppers = append(hoppers, Hopper{Facing: f, Powered: true})
	}
	return hoppers
}
