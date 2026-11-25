# Third-party notices

## SonoffLAN compatibility reference

GoHome's eWeLink consumer-account authentication compatibility logic was developed with reference to the public implementation in AlexxIT/SonoffLAN:

- Project: https://github.com/AlexxIT/SonoffLAN
- Copyright: Copyright (c) 2020 AlexxIT
- License: MIT License

The following MIT license notice applies to the referenced compatibility logic and related attribution:

MIT License

Copyright (c) 2020 AlexxIT

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.


## Xiaomi MIoT protocol references

GoHome independently implements requests using public MIoT Spec definitions and the legacy Xiaomi account API behavior documented by the community `hass-xiaomi-miot` project. No source code from Xiaomi's `ha_xiaomi_home` integration is copied into GoHome. The official integration repository contains a Xiaomi-specific license that limits its licensed work to Home Assistant for non-commercial use; review that repository's license before redistributing its source.

References:
- https://miot-spec.org/miot-spec-v2/instances?status=all
- https://github.com/al-one/hass-xiaomi-miot
- https://github.com/XiaoMi/ha_xiaomi_home
