const path = require('path');

module.exports = {
    // TODO(jsd): change to 'production' sometime
    mode: 'development',
    entry: './src/index.tsx',
    devtool: 'inline-source-map',
    module: {
        rules: [
            {
                test: /\.tsx?$/,
                exclude: /node_modules/,
                use: 'ts-loader',
            },
            {
                test: /\.css$/i,
                use: ["style-loader", "css-loader"],
            },
        ],
    },
    watchOptions: {
        poll: true,
        ignored: /node_modules/
    },
    resolve: {
        extensions: [ '.ts', '.tsx', '.js' ],
    },
    output: {
        filename: 'app.js',
        path: path.resolve(__dirname, '../dist/r'),
    },
};
