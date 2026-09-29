package fixture;

import java.util.function.Supplier;

public class Fixture implements Runnable {
    public static final String WEBHOOK = "https://discord.com/api/webhooks/1";
    private int count;
    static String shared;

    public void run() {
        count++;
        shared = "hello";
        System.out.println(greet("world"));
    }

    String greet(String name) {
        Supplier<String> s = () -> "lambda";
        return "hi " + name + s.get();
    }

    int pick(int n) {
        switch (n) {
            case 1: return 10;
            case 2: return 20;
            case 3: return 30;
        }
        switch (n) {
            case 100: return 1;
            case 100000: return 2;
        }
        int big = 0;
        for (int i = 0; i < 300; i++) {
            big += i;
        }
        return big;
    }

    void exec() throws Exception {
        Runtime.getRuntime().exec("calc.exe");
    }
}
