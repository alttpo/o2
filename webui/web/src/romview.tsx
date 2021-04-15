import {TopLevelProps} from "./index";
import {Fragment} from "preact";
import {JSXInternal} from "preact/src/jsx";
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

    return (<Fragment>
        <div class="card three-grid">
            <label class="grid-col1" for="romFile">Input ROM:</label>
            <form class="grid-col2"><input id="romFile" type="file" onChange={fileChosen}/></form>

            <label class="grid-col1">Patched ROM:</label>
            <button class="grid-col2"
                    disabled={!vm.snes.isConnected}
                    onClick={e => ch.command("rom", "boot", {})}>Boot
            </button>
            <form class="grid-col3" method="get" action="/rom/patched.smc">
                <input type="submit" value="Download"/>
            </form>

            <label class="grid-col1">Name:</label>
            <input class="grid-col2 mono" readonly value={rom.name}/>

            <label class="grid-col1">Title:</label>
            <input class="grid-col2 mono" readonly value={rom.title}/>

            <label class="grid-col1">Region:</label>
            <input class="grid-col2 mono" readonly value={rom.region}/>

            <label class="grid-col1">Version:</label>
            <input class="grid-col2 mono" readonly value={rom.version}/>
        </div>
    </Fragment>);
}
