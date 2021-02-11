import {DriverViewModel, SNESViewModel, ViewModel} from "./viewmodel";
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
                               onClick={cmdDisconnect.bind(this, drv)}>Disconnect</button>;
            } else {
                return <button type="button"
                               disabled={(snes.isConnected && !drv.isConnected) || (state.deviceIndex == 0)}
                               onClick={cmdConnect.bind(this, drv)}>Connect</button>;
            }
        };

        const {name} = drv;

        return <div class="card" key={name}>
            <h4>{drv.displayName}</h4>
            <h5>{drv.displayDescription}</h5>
            <label for={`device-${name}`}>Device</label>
            <select
                disabled={snes.isConnected && !drv.isConnected}
                id={`device-${name}`}
                onChange={(e) => this.setState({deviceIndex: (e.currentTarget.selectedIndex)})}>
                <option selected={0 == drv.selectedDevice}>(Select a SNES Device)</option>
            {(drv.devices || []).map((dev, i) =>
                <option selected={(i + 1) == drv.selectedDevice}>{dev}</option>
            )}
            </select>
            {connectButton(drv)}
        </div>;
    }
}

export default ({ch, vm}: TopLevelProps) => {
    return (
        <Fragment>{
            (vm.snes?.drivers || []).map(drv => (
                <SNESDriverView ch={ch} snes={vm.snes} drv={drv}/>
            ))
        }</Fragment>
    );
};
