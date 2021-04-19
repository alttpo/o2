import {DriverViewModel, SNESViewModel} from "./viewmodel";
import {CommandHandler, TopLevelProps} from "./index";
import {Component, Fragment} from 'preact';

type SNESDriverProps = {
    ch: CommandHandler;
    snes: SNESViewModel;
    drv: DriverViewModel;
};
type SNESDriverState = {
    deviceIndex: number;
    selectedDevice: number;
};

class SNESDriverView extends Component<SNESDriverProps, SNESDriverState> {
    constructor() {
        super();
        this.state = {deviceIndex: 0, selectedDevice: 0};
    }

    static getDerivedStateFromProps(props: SNESDriverProps, state: SNESDriverState): SNESDriverState {
        if (props.drv.selectedDevice != state.selectedDevice) {
            return {deviceIndex: props.drv.selectedDevice, selectedDevice: props.drv.selectedDevice};
        } else {
            return {deviceIndex: state.deviceIndex, selectedDevice: props.drv.selectedDevice};
        }
    }

    render({ch, snes, drv}: SNESDriverProps, state: SNESDriverState) {
        const cmdConnect = (drv: DriverViewModel, e: Event) => {
            e.preventDefault();
            ch.command('snes', 'connect', {driver: drv.name, device: state.deviceIndex});
        }
        const cmdDisconnect = (drv: DriverViewModel, e: Event) => {
            e.preventDefault();
            ch.command('snes', 'disconnect', {driver: drv.name});
        }

        const connectButton = (drv: DriverViewModel) => {
            if (drv.isConnected) {
                return <button type="button"
                               class="grid-c2-2"
                               title={drv.displayDescription}
                               onClick={cmdDisconnect.bind(this, drv)}>Disconnect</button>;
            } else {
                return <button type="button"
                               title={drv.displayDescription}
                               disabled={(snes.isConnected && !drv.isConnected) || (state.deviceIndex == 0)}
                               onClick={cmdConnect.bind(this, drv)}>Connect</button>;
            }
        };

        const {name} = drv;

        return <Fragment key={name}>
            <label class="grid-c1"
                   for={`device-${name}`}
                   title={drv.displayDescription}>{drv.displayName}:</label>
            <select
                class="grid-c2-1"
                disabled={snes.isConnected && !drv.isConnected}
                id={`device-${name}`}
                title={drv.displayDescription}
                onChange={(e) => this.setState({deviceIndex: (e.currentTarget.selectedIndex)})}>
                <option selected={0 == drv.selectedDevice}>(Select {drv.displayName} Device)</option>
                {(drv.devices || []).map((dev, i) =>
                    <option selected={(i + 1) == drv.selectedDevice}>{dev}</option>
                )}
            </select>
            {connectButton(drv)}
        </Fragment>;
    }
}

export default ({ch, vm}: TopLevelProps) => {
    return (
        <div class="grid" style="min-width: 32em">
            <h5 class="grid-ca">
                <span data-rh-at="left" data-rh="Select one of the below SNES drivers to connect to your SNES device.
Devices are auto-detected every 2 seconds for each driver."
                >Select a SNES device:&nbsp;1️⃣</span>
            </h5>
            {
                (vm.snes?.drivers || []).map(drv => (
                    <SNESDriverView ch={ch} snes={vm.snes} drv={drv}/>
                ))
            }
        </div>
    );
};
