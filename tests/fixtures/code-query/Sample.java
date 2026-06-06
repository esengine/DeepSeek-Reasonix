public class Sample {
    private final String name;

    public Sample(String name) {
        this.name = name;
    }

    public String greet() {
        return "hi " + name;
    }

    public static void main(String[] args) {
        Sample s = new Sample("world");
        System.out.println(s.greet());
    }
}
