in theory, and this took a little big of digging through alttp asm code to determine, it should be possible, with a lot of work, to sync enemy state among multiple players.

however, there are some concerns on exactly how to accomplish it.

first, some background. there can be at most 16 enemies/sprites in an area. each area defines its own set of enemies/sprites and assigns each an ID from 1 to 16. when a sprite is spawned in a slot, its ID is recorded into that slot. the slot index and the sprite IDs are unrelated otherwise. the enemies could be in different sprite slots for different players, so the IDs need to be used to identify the same enemy across multiple players.

alttp tracks the death of all enemies on both the overworld ($7FEF80) and underworld ($7FDF80) for every overworld area and underworld supertile. each enemy having a unique sprite ID is assigned a single bit in each area's bitfield. with 16 enemies that means 2 bytes per area.

the death flags can be easily synced among players but there are times where alttp resets all the deaths and brings back all previously killed enemies like when transitioning from underworld to overworld or vice versa. how to sync this full reset (or if it's even desirable to sync) may be problematic. if it's synced additively (like most things), then inevitably every enemy on every screen will wind up permanently dead and never come back. this might be cool or might be bad.

## syncing enemy behavior
alttp first renders an enemy based on its current state and then executes its AI behavior _after_ rendering to alter that state for the next frame's rendering. this ordering is important because it means if we copy in an enemy's state at the beginning of a frame, the game will render it in exactly that state but still also execute the AI behavior so that new state can be captured and synced to other players.

it's now a matter of figuring out which player should "own" a given enemy when multiple players are in the same area observing that same enemy. there are several approaches to this we could take.

idea #1: whoever spawns the enemy owns the enemy for all time. this would make the enemy only be aware of the player who spawned it; other players could still attack it in cooperation with the owning player, but it would never retaliate to that other attacking player. this is very simple to implement and the lack of awareness of other players may or may not be an issue depending on the enemy type.

idea #2: whoever spawns the enemy owns the enemy but we change the owner based on whichever player is closest to the enemy for a continuous but short period of time, say 60 frames. this may work for several enemy types but maybe not others (e.g. leevers, wizzrobes).

idea #3: whoever spawns the enemy owns the enemy but we change the owner to whoever attacked it last. this would make enemies naturally aggro against the last attacker obviously, but they'd still ignore all other players in the area.

TBD: how to handle remote players attacking enemies they do not own.

## technical hurdles
focusing on console compatibility, we must devise a way to _reliably_ copy out the enemy state from WRAM when it's safe to do so. why would it be unsafe? the fx pak pro can issue WRAM reads at any point during the frame cycle but from the PC side it's very difficult to know _when_ relative to the frame cycle to issue the WRAM read. we could read while enemy state is being adjusted by the game, which would be bad; but on the other hand we could read during some other phase of the game's execution which would be safe. how can we tell the difference?

we can try to sync up our WRAM reading to the frame cycle by altering the game a little bit. what if, before the sprite logic executes, we mark an unused byte in WRAM as $01 and then when the sprite logic is done executing we reset that byte back to $00. now, when we do our reads, we just read that byte last so that we know if all the data we read was during a good time or a bad time.

ok so now we can know if we had a good or a bad read, but how often will we get a good read? we could do a little timing sync loop to try to sync up with the game's frame cycle and adjust our delay between reads to minimize bad reads. this will only work if we can fit *all* our data reads into a single VGET which only allows 8 individual 255-byte-sized reads. each USB request-response cycle is at best 4ms so we don't have much wiggle room in a 16.7ms frame window to do multiple VGETs. this severely limits us in terms of memory transfer bandwidth from the SNES to the PC. we also have to occassionally VPUT our generated ASM code to write WRAM so it's looking like perfect per-frame enemy sync may be just out of reach. some prototyping will be necessary to determine if this is the case or not.
