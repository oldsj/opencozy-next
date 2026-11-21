package org.opencozy.display;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import org.junit.Test;

public final class ServerAddressTest {
    @Test
    public void takesWhatPeopleType() {
        assertEquals("https://family.example.test", ServerAddress.parse("family.example.test").origin());
        assertEquals("https://family.example.test", ServerAddress.parse(" HTTPS://Family.Example.Test/ ").origin());
        assertEquals("https://family.example.test:8443", ServerAddress.parse("family.example.test:8443").origin());
        assertEquals("https://family.example.test", ServerAddress.parse("https://family.example.test:443").origin());
        assertEquals("https://family.example.test/display", ServerAddress.parse("family.example.test").displayUrl());
    }

    @Test
    public void refusesAnythingButAnHttpsOrigin() {
        String[] bad = {
            "", "   ", "http://family.example.test", "https://family.example.test/display",
            "https://user:pass@family.example.test", "https://family.example.test/?a=1",
            "https://family.example.test/#x", "file:///sdcard/x", "javascript:alert(1)", "https://family.example.test:99999",
        };
        for (String b : bad) {
            try {
                ServerAddress.parse(b);
                fail("accepted " + b);
            } catch (IllegalArgumentException expected) {
                // refused
            }
        }
    }

    @Test
    public void allowsOnlyItsOwnOrigin() {
        ServerAddress a = ServerAddress.parse("family.example.test");
        assertTrue(a.allows("https://family.example.test/display"));
        assertTrue(a.allows("https://FAMILY.example.test:443/kid#rewards"));
        assertFalse(a.allows("http://family.example.test/display"));
        assertFalse(a.allows("https://family.example.test:8443/"));
        assertFalse(a.allows("https://evil.example.test/"));
        assertFalse(a.allows("https://family.example.test.evil.example.test/"));
        assertFalse(a.allows("https://user@family.example.test/"));
        assertFalse(a.allows("intent://family.example.test/#Intent;end"));
        assertFalse(a.allows(null));
    }
}
