
## docker部署
推荐用macvlan，可使用以下一键脚本来配置和宿主机通信
```bash
bash <(wget -qO- https://ghproxy.net/https://raw.githubusercontent.com/KyleYu2024/Script/main/macvlan_setup.sh)
```
```yaml
services:
  mosctl:
    image: kyleyu2024/mosctl:latest
    container_name: mosctl
    restart: always
    ports:
      - "53:53/udp"
      - "53:53/tcp"
    environment:
      LOCAL_UPSTREAM: "udp://223.5.5.5" #国内上游dns
      REMOTE_UPSTREAM: "udp://10.10.1.202:53" #国外上游dns
      TZ: "Asia/Shanghai"
    volumes:
      - ./data:/etc/mosdns
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
    networks:
      my_macvlan:
        ipv4_address: 10.10.1.201
networks:
  my_macvlan:
    external: true
    name: macvlan
```
     

