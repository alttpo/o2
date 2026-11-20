import {DriverViewModel, SNESViewModel} from "./viewmodel";
import {CommandHandler, TopLevelProps} from "./index";
import {Component, Fragment} from 'preact';
import {useState} from "preact/hooks";

type SNESDriverProps = {
    ch: CommandHandler;
    snes: SNESViewModel;
    drv: DriverViewModel;
};
type SNESDriverState = {
    deviceIndex: string;
    selectedDevice: string;
};

// deviceCountLabel is the label of the first option of a driver's device list, which says how many devices were found
// so that a real device name stands out:
function deviceCountLabel(n: number): string {
    switch (n) {
        case 0:
            return "--- no devices ---";
        case 1:
            return "--- 1 device ---";
        default:
            return `--- ${n} devices ---`;
    }
}

class SNESDriverView extends Component<SNESDriverProps, SNESDriverState> {
    constructor() {
        super();
        this.state = {deviceIndex: "", selectedDevice: ""};
    }

    static getDerivedStateFromProps(props: SNESDriverProps, state: SNESDriverState): SNESDriverState {
        const devices = props.drv.devices || [];
        if (props.drv.selectedDevice != state.selectedDevice && props.drv.selectedDevice != "") {
            // the server chose a device (connected, or loaded from the saved configuration):
            return {deviceIndex: props.drv.selectedDevice, selectedDevice: props.drv.selectedDevice};
        }

        // keep the user's choice while it is still detected, otherwise select the first available device:
        let deviceIndex = state.deviceIndex;
        if (!devices.some(dv => dv.id == deviceIndex)) {
            deviceIndex = devices.length > 0 ? devices[0].id : "";
        }
        return {deviceIndex, selectedDevice: props.drv.selectedDevice};
    }

    render({ch, snes, drv}: SNESDriverProps, state: SNESDriverState) {
        const cmdConnect = (drv: DriverViewModel, e: Event) => {
            e.preventDefault();
            ch.command('snes', 'connect', {
                driver: drv.name,
                device: drv.devices.find(dv => dv.id == state.deviceIndex)
            });
        }
        const cmdDisconnect = (drv: DriverViewModel, e: Event) => {
            e.preventDefault();
            ch.command('snes', 'disconnect', {driver: drv.name});
        }

        const connectButton = (drv: DriverViewModel) => {
            if (drv.isConnected) {
                return <button type="button"
                               style="width: 7em"
                               title={drv.displayDescription}
                               onClick={cmdDisconnect.bind(this, drv)}>Disconnect</button>;
            } else {
                return <button type="button"
                               style="width: 7em"
                               title={drv.displayDescription}
                               disabled={(snes.isConnected && !drv.isConnected) || (state.deviceIndex == "")}
                               onClick={cmdConnect.bind(this, drv)}>Connect</button>;
            }
        };

        const {name} = drv;
        const deviceCount = drv.devices?.length || 0;

        return <Fragment key={name}>
            <label for={`device-${name}`}
                   style="white-space: nowrap; padding-top: 0.35em"
                   title={drv.displayDescription}
            >{drv.displayName}:{name == "fxpakpro" && <Fragment>
                &nbsp;<span class="hint-icon"
                            data-rhl="For FX Pak Pro, use the"
                            data-rhl-link="latest firmware"
                            data-rhl-href="https://sd2snes.de/blog/"
                            data-rhl-at="right">ⓘ</span>
            </Fragment>}</label>
            <select
                disabled={snes.isConnected || deviceCount == 0}
                style="min-width: 0; width: 100%; box-sizing: border-box"
                id={`device-${name}`}
                title={drv.displayDescription}
                value={state.deviceIndex}
                onChange={(e) => this.setState({deviceIndex: (e.currentTarget.value)})}>
                <option value="">{deviceCountLabel(deviceCount)}</option>
                {(drv.devices || []).map(dev =>
                    <option value={dev.id}
                    >{dev.displayName}</option>
                )}
            </select>
            {connectButton(drv)}
        </Fragment>;
    }
}

export default ({ch, vm}: TopLevelProps) => {
    const [collapsed, set_collapsed] = useState(false);

    return (<div class={"collapsible" + (collapsed ? " collapsed" : "")} style="display: table; width: 100%; height: 100%">
        <h5>
                <span data-rh-at="left" data-rh="Select one of the below SNES drivers to connect to your SNES device.
Devices are auto-detected every 2 seconds for each driver."
                >Select a SNES device:&nbsp;1️⃣</span>
            <span class="collapse-icon" onClick={() => set_collapsed(st => !st)}>{ collapsed ? "🔽": "🔼" }</span>
        </h5>
        <div class="grid" style="grid-template-columns: max-content minmax(0, 1fr) max-content;">
            {
                (vm.snes?.drivers || []).map(drv => (
                    <SNESDriverView ch={ch} snes={vm.snes} drv={drv}/>
                ))
            }
        </div>
    </div>);
};
