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

    return (<div style="min-width: 22em; width: 100%; height: 100%">
        <div class="grid" style="grid-template-columns: 1fr 1fr 1fr">
            <h5 style="grid-column: 1 / span 3">
        <span data-rh-at="left" data-rh="O2 needs to know which game you want to play. This
is determined only by the ROM that you select. O2 requires that the ROM you play on your SNES to be
patched for O2 support. O2 automatically patches your Input ROM for you."
        >Select a game ROM:&nbsp;2️⃣</span>
            </h5>
            <label for="romFile">Input ROM:</label>
            <form style="grid-column-end: span 2">
                <input id="romFile"
                       type="file"
                       title="Select a game ROM to play and patch for O2 support"
                       onChange={fileChosen}
                />
            </form>

            <label>Name:</label>
            <input style="grid-column-end: span 2" class="mono" readonly value={rom?.name}/>

            <label>Title:</label>
            <input style="grid-column-end: span 2" class="mono" readonly value={rom?.title}/>

            <label>Version:</label>
            <input style="grid-column-end: span 2" class="mono" readonly value={rom?.region + " " + rom?.version}/>

            <label><span data-rh-at="left" data-rh="O2 can only communicate with patched ROMs running
on SNES devices. Either click 'Boot' to send the ROM to your SNES device if supported or click 'Download' to download
the patched ROM and manually send it to your SNES device.">Patched ROM:&nbsp;3️⃣</span></label>
            <button style=""
                    disabled={!rom?.isLoaded || !vm.snes?.isConnected}
                    title="Send the O2 patched ROM to the SNES and boot it"
                    onClick={e => ch.command("rom", "boot", {})}>Boot
            </button>
            <form method="get" action="/rom/patched.smc">
                <input type="submit"
                       disabled={!rom?.isLoaded}
                       title="Download the O2 patched ROM"
                       value="Download"/>
            </form>
        </div>
    </div>);
}
