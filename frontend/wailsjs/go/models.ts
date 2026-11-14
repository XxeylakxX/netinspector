export namespace main {
	
	export class WiFi {
	    ssid: string;
	    standard: string;
	    band: string;
	    channel: string;
	    channelWidth?: string;
	    rxRate: string;
	    txRate: string;
	    signal: string;
	    signalDbm?: string;
	    auth: string;
	
	    static createFrom(source: any = {}) {
	        return new WiFi(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ssid = source["ssid"];
	        this.standard = source["standard"];
	        this.band = source["band"];
	        this.channel = source["channel"];
	        this.channelWidth = source["channelWidth"];
	        this.rxRate = source["rxRate"];
	        this.txRate = source["txRate"];
	        this.signal = source["signal"];
	        this.signalDbm = source["signalDbm"];
	        this.auth = source["auth"];
	    }
	}
	export class Adapter {
	    alias: string;
	    desc: string;
	    status: string;
	    media: string;
	    mac: string;
	    linkSpeed: string;
	    ipv4: string[];
	    ipv6: string[];
	    gateway: string[];
	    dns: string[];
	    isWifi: boolean;
	    wifi?: WiFi;
	
	    static createFrom(source: any = {}) {
	        return new Adapter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.alias = source["alias"];
	        this.desc = source["desc"];
	        this.status = source["status"];
	        this.media = source["media"];
	        this.mac = source["mac"];
	        this.linkSpeed = source["linkSpeed"];
	        this.ipv4 = source["ipv4"];
	        this.ipv6 = source["ipv6"];
	        this.gateway = source["gateway"];
	        this.dns = source["dns"];
	        this.isWifi = source["isWifi"];
	        this.wifi = this.convertValues(source["wifi"], WiFi);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DNSPreset {
	    id: string;
	    name: string;
	    primary: string;
	    secondary: string;
	    dohTemplate: string;
	    isDhcp: boolean;
	    isCustom: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DNSPreset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.primary = source["primary"];
	        this.secondary = source["secondary"];
	        this.dohTemplate = source["dohTemplate"];
	        this.isDhcp = source["isDhcp"];
	        this.isCustom = source["isCustom"];
	    }
	}
	export class RouterLocation {
	    name: string;
	    username: string;
	    password: string;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new RouterLocation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.url = source["url"];
	    }
	}
	export class SpeedtestServer {
	    id: number;
	    name: string;
	    location: string;
	    country: string;
	    host: string;
	
	    static createFrom(source: any = {}) {
	        return new SpeedtestServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.location = source["location"];
	        this.country = source["country"];
	        this.host = source["host"];
	    }
	}

}

