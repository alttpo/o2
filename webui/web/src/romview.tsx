import {TopLevelProps} from "./index";
import {JSXInternal} from "preact/src/jsx";
import TargetedEvent = JSXInternal.TargetedEvent;

export default ({ch, vm}: TopLevelProps) => {
    function fileChosen(e: TargetedEvent<HTMLInputElement, Event>) {
        e.currentTarget.files[0].arrayBuffer().then(buf => {
            ch.binaryCommand('rom', 'chosen', buf);
        });
    }

    return (
        <div class="card">
            <label for="romFile">ROM:</label><input id="romFile" type="file" onChange={(e) => fileChosen(e)}/>
        </div>
    );
};
