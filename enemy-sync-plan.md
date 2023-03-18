in theory, and this took a little big of digging through alttp asm code to determine, it should be possible, with a lot of work, to sync enemy state among multiple players.

however, there are some concerns on exactly how to accomplish it.

first, some background. there can be at most 16 enemies/sprites in an area. each area defines its own set of enemies/sprites and assigns each an ID from 1 to 16. when a sprite is spawned in a slot, its ID is recorded into that slot. the slot index and the sprite IDs are unrelated otherwise. the enemies could be in different sprite slots for different players, so the IDs need to be used to identify the same enemy across multiple players.

alttp tracks the death of all enemies on both the overworld ($7FEF80) and underworld ($7FDF80) for every overworld area and underworld supertile. each enemy having a unique sprite ID is assigned a single bit in each area's bitfield. with 16 enemies that means 2 bytes per area.

the death flags can be easily synced among players but there are times where alttp resets all the deaths and brings back all previously killed enemies like when transitioning from underworld to overworld or vice versa. how to sync this full reset (or if it's even desirable to sync) may be problematic. if it's synced additively (like most things), then inevitably every enemy on every screen will wind up permanently dead and never come back. this might be cool or might be bad.

** syncing enemy behavior **
alttp first renders an enemy based on its current state and then executes its AI behavior _after_ rendering to alter that state for the next frame's rendering. this ordering is important because it means if we copy in an enemy's state at the beginning of a frame, the game will render it in exactly that state but still also execute the AI behavior so that new state can be captured and synced to other players.

it's now a matter of figuring out which player should "own" a given enemy when multiple players are in the same area observing that same enemy. there are several approaches to this we could take.

idea #1: whoever spawns the enemy owns the enemy for all time. this would make the enemy only be aware of the player who spawned it; other players could still attack it in cooperation with the owning player, but it would never retaliate to that other attacking player. this is very simple to implement and the lack of awareness of other players may or may not be an issue depending on the enemy type.

idea #2: whoever spawns the enemy owns the enemy but we change the owner based on whichever player is closest to the enemy for a continuous but short period of time, say 60 frames. this may work for several enemy types but maybe not others (e.g. leevers, wizzrobes).

idea #3: whoever spawns the enemy owns the enemy but we change the owner to whoever attacked it last. this would make enemies naturally aggro against the last attacker obviously, but they'd still ignore all other players in the area.

TBD: how to handle remote players attacking enemies they do not own.
