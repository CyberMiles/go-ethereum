# Geth with ewasm support

The fork of Geth (go-ethereum) with ewasm and EVMC support.
Used for ewasm testnet, see https://github.com/ewasm/testnet for more information.


## Go Ethereum

Forked from [tendermint/go-ethereum](https://github.com/tendermint/go-ethereum) for [Lity](https://github.com/cybermiles/lity).

## Prerequisites

- [libeni](https://github.com/cybermiles/libeni)
- go >= 1.9

## Building the source

Once the dependencies are installed, run

    make geth

or, to build the full suite of utilities:

    make all

## Usage

- [Official geth project](https://github.com/ethereum/go-ethereum)
- [Official geth CLI wiki page](https://github.com/ethereum/go-ethereum/wiki/Command-Line-Options)

## Attach to a Travis node

- [Install Travis](https://github.com/CyberMiles/travis)
- [Run a Travis node](https://github.com/CyberMiles/travis)
- Attach to a Travis node:

```
$ geth attach http://localhost:8545
```

## Compile EVM bytecode

```
$ evm compile ...
```

## Run EVM bytecode

```
$ evm run ...
```

## License

The go-ethereum library (i.e. all code outside of the `cmd` directory) is licensed under the
[GNU Lesser General Public License v3.0](https://www.gnu.org/licenses/lgpl-3.0.en.html), also
included in our repository in the `COPYING.LESSER` file.

The go-ethereum binaries (i.e. all code inside of the `cmd` directory) is licensed under the
[GNU General Public License v3.0](https://www.gnu.org/licenses/gpl-3.0.en.html), also included
in our repository in the `COPYING` file.
