package io.nekohasekai.sagernet.group

import android.app.Application
import android.util.Base64
import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.fmt.mieru.MieruBean
import io.nekohasekai.sagernet.fmt.mieru.MieruFmtTest
import io.nekohasekai.sagernet.fmt.mieru.MieruLogShadow
import io.nekohasekai.sagernet.fmt.mieru.toMieruOutbound
import io.nekohasekai.sagernet.ktx.parseProxies
import kotlinx.coroutines.runBlocking
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], application = Application::class, shadows = [MieruLogShadow::class], instrumentedPackages = ["libcore"])
class MieruSubscriptionTest {
    @Test fun clashSubscriptionImportsNativeTcpUdpAndPortRanges() = runBlocking {
        val nodes = RawUpdater.parseRaw("""
            proxies:
              - name: TCP node
                type: mieru
                server: example.com
                port: 443
                transport: TCP
                username: user
                password: pass
                multiplexing: MULTIPLEXING_HIGH
              - name: UDP node
                type: mieru
                server: 2001:db8::1
                port-range: 8443-8445
                transport: UDP
                username: user
                password: pass
                mtu: 1300
        """.trimIndent())!!
        assertEquals(2, nodes.size)
        val tcp = nodes[0] as MieruBean
        val udp = nodes[1] as MieruBean
        assertEquals("TCP node", tcp.name)
        assertEquals(443, tcp.toMieruOutbound().server_port.toInt())
        assertEquals("MULTIPLEXING_HIGH", tcp.multiplexing)
        assertEquals("UDP", udp.protocol)
        assertEquals(listOf("8443-8445"), udp.toMieruOutbound().server_ports)
        assertEquals(1300, udp.mtu.toInt())
        for (node in nodes) assertFalse(ProxyEntity().apply { putBean(node) }.needExternal())
    }

    @Test fun malformedMieruNodeDoesNotDiscardMixedYamlSubscription() = runBlocking {
        val nodes = RawUpdater.parseRaw("""
            proxies:
              - {name: broken, type: mieru, server: example.com, port-range: 9000-8000, username: user, password: pass}
              - {name: valid, type: mieru, server: example.com, port: 443, username: user, password: pass}
              - {name: socks, type: socks5, server: example.org, port: 1080}
        """.trimIndent())!!
        assertEquals(listOf("valid", "socks"), nodes.map { it.name })
    }

    @Test fun singBoxNumericPortAndRangeOnlyObjectAreRecognized() = runBlocking {
        val numeric = RawUpdater.parseRaw("""{"type":"mieru","tag":"numeric","server":"example.com","server_port":443,"username":"user","password":"pass"}""")!!.single() as MieruBean
        assertEquals("numeric", numeric.name)
        assertEquals(443, numeric.serverPort.toInt())
        assertTrue(numeric.serverPorts.isEmpty())
        val ranges = RawUpdater.parseRaw("""{"type":"mieru","server":"example.com","server_ports":["8000-8002"],"transport":"UDP","username":"user","password":"pass"}""")!!.single() as MieruBean
        assertEquals(listOf("8000-8002"), ranges.serverPorts)
    }

    @Test fun malformedJsonNodeDoesNotDiscardOtherOutbounds() {
        val nodes = RawUpdater.parseJSON(JSONObject("""{"outbounds":[
            {"type":"direct"},
            {"type":"mieru","server":"bad.example","server_port":0,"username":"user","password":"pass"},
            {"type":"mieru","tag":"valid","server":"example.com","server_port":443,"username":"user","password":"pass"},
            {"type":"socks","tag":"other","server":"example.org","server_port":1080}
        ]}"""))
        assertEquals(listOf("valid", "other"), nodes.map { it.name })
    }

    @Test fun plaintextAndBase64SubscriptionsAcceptBothOfficialFormats() = runBlocking {
        val text = MieruFmtTest.SIMPLE + "\n" + MieruFmtTest.PROTOBUF
        assertEquals(2, parseProxies(text).filterIsInstance<MieruBean>().size)
        for (flags in listOf(Base64.NO_WRAP, Base64.NO_WRAP or Base64.URL_SAFE or Base64.NO_PADDING)) {
            val encoded = Base64.encodeToString(text.toByteArray(), flags)
            val nodes = RawUpdater.parseRaw(encoded)!!.filterIsInstance<MieruBean>()
            assertEquals(2, nodes.size)
            assertTrue(nodes.all { it.name == "Official fixture" && it.username == "user:name" })
        }
    }

    @Test fun malformedLinkDoesNotDiscardValidSubscriptionEntries() = runBlocking {
        val nodes = parseProxies("mierus://broken\n" + MieruFmtTest.SIMPLE)
        assertEquals(1, nodes.size)
        assertEquals("Official fixture", nodes.single().name)
    }
}
