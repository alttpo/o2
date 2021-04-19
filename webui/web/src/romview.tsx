import {JSXInternal} from "preact/src/jsx";

import {TopLevelProps} from "./index";
import TargetedEvent = JSXInternal.TargetedEvent;

export default ({ch, vm}: TopLevelProps) => {
    const rom = vm.rom;

    function fileChosen(e: TargetedEvent<HTMLInputElement, Event>) {
        // send ROM filename and contents:
        let file = e.currentTarget.files[0];
        file.arrayBuffer().then(buf => {
            ch.command('rom', 'name', {name: file.name});
            ch.binaryCommand('rom', 'data', buf);
        });
        e.currentTarget.form.reset();
    }

    return (
        <div class="grid" style="min-width: 20em">
            <h5 class="grid-ca">
                <span data-rh-at="left" data-rh="O2 needs to know which game you want to play. This
is determined only by the ROM that you select. O2 also must patch the ROM that you select
so that it can write to the game's memory."
                >Select a game ROM:&nbsp;2️⃣</span>
            </h5>
            <label class="grid-c1" for="romFile">Input ROM:</label>
            <form class="grid-c2">
                <input id="romFile"
                       type="file"
                       title="Select a game ROM to play and patch for O2 support"
                       onChange={fileChosen}
                />
            </form>

            <label class="grid-c1">Name:</label>
            <input class="grid-c2 mono" readonly value={rom.name}/>

            <label class="grid-c1">Title:</label>
            <input class="grid-c2 mono" readonly value={rom.title}/>

            <label class="grid-c1">Version:</label>
            <input class="grid-c2 mono" readonly value={rom.region + " " + rom.version}/>

            <label class="grid-c1"><span data-rh-at="left" data-rh="O2 can only communicate with patched ROMs running
on SNES devices. Either click 'Boot' to send the ROM to your SNES device if supported or click 'Download' to download
the patched ROM and manually send it to your SNES device.">Patched ROM:&nbsp;3️⃣</span></label>
            <button class="grid-c2-1"
                    disabled={!rom.isLoaded || !vm.snes.isConnected}
                    title="Send the O2 patched ROM to the SNES and boot it"
                    onClick={e => ch.command("rom", "boot", {})}>Boot
            </button>
            <form class="grid-c2-2" method="get" action="/rom/patched.smc">
                <input type="submit"
                       disabled={!rom.isLoaded}
                       title="Download the O2 patched ROM"
                       value="Download"/>
            </form>
        </div>
    );
}
