import {TopLevelProps} from "./index";
import {JSXInternal} from "preact/src/jsx";
import TargetedEvent = JSXInternal.TargetedEvent;

export default ({ch, vm}: TopLevelProps) => {
    function fileChosen(e: TargetedEvent<HTMLInputElement, Event>) {
        // send ROM filename and contents:
        let file = e.currentTarget.files[0];
        file.arrayBuffer().then(buf => {
            ch.command('rom', 'name', {name: file.name});
            ch.binaryCommand('rom', 'data', buf);
        });
    }

    return (
        <div class="card">
            <label for="romFile">ROM:</label><input id="romFile" type="file" onChange={(e) => fileChosen(e)}/>
        </div>
    );
};
